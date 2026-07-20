import AVFoundation
import Foundation
import Observation
import Speech

/// Запись голосового ввода расхода (hold-to-talk) с живой транскрипцией.
///
/// `AVAudioEngine` с tap на входе: один и тот же поток буферов идёт
/// (1) в `SFSpeechRecognizer` — живой текст в оверлее записи (best-effort:
/// нет разрешения/языка — просто без текста), и (2) через `AVAudioConverter`
/// в PCM 16 кГц/16 бит/моно — на стопе оборачивается WAV-заголовком.
/// Формат отправки — `audio/wav` (в серверном allowlist): ~32 КБ/с,
/// минута речи ≈ 1.9 МБ при серверном лимите 3 МБ.
@MainActor
@Observable
final class AudioRecorder {
    /// true — идёт запись (для подсветки кнопки-микрофона и оверлея).
    private(set) var isRecording = false
    /// Момент старта записи — для кольца-прогресса лимита и автостопа.
    private(set) var startedAt: Date?
    /// Данные последней записи (WAV) — их шлёт `APIClient.parseOperation(audio:)`.
    private(set) var audioData: Data?
    /// Живая транскрипция текущей записи (пусто, если Speech недоступен).
    private(set) var transcript = ""
    /// Текущая громкость голоса 0…1 (RMS по буферу) — волна и «дыхание»
    /// микрофона в оверлее реагируют на реальный звук, а не на синтетику.
    private(set) var level: Float = 0

    /// MIME записанного аудио.
    let mimeType = "audio/wav"

    /// Запись оборвана системой (входящий звонок, сброс медиасервисов).
    /// Вью гасит оверлей и замок: в закреплённом режиме пальца нет, отмену
    /// касания UIKit не пришлёт, и «Запись идёт» висела бы над мёртвым движком,
    /// а автостоп через минуту отправлял бы ОБРЕЗАННЫЙ WAV как полный.
    var onInterrupted: (() -> Void)?

    /// Подписки на события аудиосессии. Коробка нужна, чтобы снять их в
    /// deinit: он nonisolated и до main-actor свойств не дотягивается.
    private let sessionObservers = ObserverBox()

    init() {
        observeSessionEvents()
    }

    /// Прерывание сессии (звонок) и сброс медиасервисов: движок остановлен
    /// системой — приводим состояние в порядок и выбрасываем обрывок.
    private func observeSessionEvents() {
        let center = NotificationCenter.default
        let onInterruption = center.addObserver(
            forName: AVAudioSession.interruptionNotification,
            object: nil,
            queue: .main
        ) { [weak self] note in
            let raw = note.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt
            guard raw == AVAudioSession.InterruptionType.began.rawValue else { return }
            MainActor.assumeIsolated { self?.abortByInterruption() }
        }
        let onReset = center.addObserver(
            forName: AVAudioSession.mediaServicesWereResetNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { self?.abortByInterruption() }
        }
        sessionObservers.tokens = [onInterruption, onReset]
    }

    private func abortByInterruption() {
        guard isRecording || isStarting else { return }
        stop()
        reset()
        onInterrupted?()
    }

    private var engine: AVAudioEngine?
    private let sink = PCMSink()
    private var speechRecognizer: SFSpeechRecognizer?
    private var speechTask: SFSpeechRecognitionTask?
    /// Стабильная точка подачи буферов в Speech: tap захватывает её один раз,
    /// а запрос внутри подменяется при перезапуске сегмента распознавания.
    private let speechFeed = SpeechFeed()
    /// Текст закрытых системой сегментов распознавания: iOS финализирует
    /// сегмент после паузы в речи и начинает новый с чистого листа — без
    /// накопления в транскрипте оставался бы только хвост.
    private var finalizedTranscript = ""
    /// Кэш решения по Speech-доступу: nil — ещё не спрашивали.
    private var speechAllowed: Bool?
    /// Сколько сегментов распознавания подряд упало с ошибкой — ограничитель
    /// перезапусков, чтобы не крутить бесконечный цикл на мёртвом распознавателе.
    private var speechSegmentFailures = 0
    private static let maxSpeechSegmentFailures = 3

    /// Доступ к микрофону: УЖЕ выданный статус читается синхронно (без
    /// асинхронного TCC-запроса на каждой новой форме — он давал задержку);
    /// системный prompt — только при первом использовании вообще.
    func ensurePermission() async -> Bool {
        switch AVAudioApplication.shared.recordPermission {
        case .granted:
            return true
        case .undetermined:
            return await withCheckedContinuation { continuation in
                AVAudioApplication.requestRecordPermission { granted in
                    continuation.resume(returning: granted)
                }
            }
        default:
            return false
        }
    }

    /// true — старт записи уже идёт (подъём аудиосессии в фоне).
    private var isStarting = false

    /// Начинает запись (hold-to-talk: вызывать по нажатию). Подъём аудиосессии
    /// и движка — В ФОНЕ: на главном потоке они замораживали отрисовку оверлея
    /// на сотни миллисекунд («дикая задержка» при нажатии). Бросает, если
    /// движок не поднялся. Транскрипция — best-effort и старт не блокирует.
    func start() async throws {
        guard !isRecording else { return } // запись уже идёт — оверлей живой
        // Параллельный старт ещё поднимает движок: молча вернуться нельзя —
        // вызывающий остался бы с «защёлкнутым» оверлеем без записи, а он
        // перехватывает касания и форма замирала навсегда.
        guard !isStarting else { throw AudioRecorderError.alreadyStarting }
        isStarting = true
        defer { isStarting = false }
        audioData = nil
        transcript = ""
        sink.reset()

        let feed = speechFeed
        let sink = sink

        let box = try await Task.detached(priority: .userInitiated) {
            try Self.makeEngine(feed: feed, sink: sink)
        }.value

        guard !isRecording else { // параллельный старт успел раньше
            box.engine.inputNode.removeTap(onBus: 0)
            box.engine.stop()
            return
        }
        self.engine = box.engine
        sink.onLevel = { [weak self] lvl in
            Task { @MainActor in
                guard let self, self.isRecording else { return }
                self.level = lvl
            }
        }
        startedAt = Date()
        isRecording = true
        // Speech — ВНЕ критического пути старта: первая инициализация
        // (XPC к демону/он-девайс модель) стоит сотни мс и блокировала бы
        // отрисовку оверлея. Потеря транскрипта первых ~0.2 c некритична —
        // аудио в WAV пишется с самого старта движка.
        Task { [weak self] in
            self?.startSpeechIfAllowed()
        }
    }

    /// Поднимает аудиосессию и движок с tap'ом (фоновый поток — дорого для main).
    private nonisolated static func makeEngine(feed: SpeechFeed, sink: PCMSink) throws -> EngineBox {
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.record, mode: .default)
        try session.setActive(true)
        // Сессия уже активна и заглушила чужую музыку: КАЖДЫЙ выход по ошибке
        // ниже обязан её отпустить, иначе плеер пользователя остаётся немым
        // до перезапуска приложения (стоп-путь такой же).
        func releaseSession() {
            try? session.setActive(false, options: .notifyOthersOnDeactivation)
        }

        let engine = AVAudioEngine()
        let input = engine.inputNode
        let inFormat = input.outputFormat(forBus: 0)
        guard inFormat.sampleRate > 0,
              let outFormat = AVAudioFormat(
                  commonFormat: .pcmFormatInt16, sampleRate: 16_000,
                  channels: 1, interleaved: true
              ),
              let converter = AVAudioConverter(from: inFormat, to: outFormat)
        else {
            releaseSession()
            throw AudioRecorderError.failedToStart
        }
        let ratio = 16_000.0 / inFormat.sampleRate

        input.installTap(onBus: 0, bufferSize: 4096, format: inFormat) { buffer, _ in
            feed.append(buffer)
            let capacity = AVAudioFrameCount(Double(buffer.frameLength) * ratio) + 16
            guard let out = AVAudioPCMBuffer(pcmFormat: outFormat, frameCapacity: capacity) else { return }
            var convertError: NSError?
            var served = false
            converter.convert(to: out, error: &convertError) { _, status in
                if served {
                    status.pointee = .noDataNow
                    return nil
                }
                served = true
                status.pointee = .haveData
                return buffer
            }
            guard convertError == nil, out.frameLength > 0,
                  let channel = out.int16ChannelData else { return }
            sink.append(Data(bytes: channel[0], count: Int(out.frameLength) * 2))

            // Уровень громкости (RMS каждого 4-го сэмпла — дёшево и достаточно).
            let count = Int(out.frameLength)
            var acc: Float = 0
            for i in stride(from: 0, to: count, by: 4) {
                let v = Float(channel[0][i])
                acc += v * v
            }
            let rms = (acc / Float(max(1, (count + 3) / 4))).squareRoot()
            let db = 20 * log10(max(rms, 1) / 32_768)
            sink.publishLevel(max(0, min(1, (db + 50) / 42)))
        }

        engine.prepare()
        do {
            try engine.start()
        } catch {
            input.removeTap(onBus: 0)
            releaseSession()
            throw AudioRecorderError.failedToStart
        }
        return EngineBox(engine: engine)
    }

    /// Останавливает запись (отпустили микрофон), собирает WAV в `audioData`.
    /// Возвращает данные или nil, если запись не велась/пуста.
    @discardableResult
    func stop() -> Data? {
        guard isRecording, let engine else { return nil }
        engine.inputNode.removeTap(onBus: 0)
        engine.stop()
        self.engine = nil
        stopSpeech()
        isRecording = false
        startedAt = nil
        level = 0

        let session = AVAudioSession.sharedInstance()
        try? session.setActive(false, options: .notifyOthersOnDeactivation)

        let pcm = sink.drain()
        guard !pcm.isEmpty else { return nil }
        audioData = Self.wav(pcm: pcm, sampleRate: 16_000)
        return audioData
    }

    /// Сбрасывает записанное аудио и транскрипт (после отправки/отмены).
    func reset() {
        audioData = nil
        transcript = ""
    }

    // MARK: Speech (живая транскрипция, best-effort)

    /// Поднимает распознавание речи для текущей записи. Статус разрешения
    /// читается СИНХРОННО (иначе первая запись каждой новой формы шла бы без
    /// транскрипта); системный prompt — только при первом использовании вообще,
    /// тогда транскрипция появится со следующей записи (запись идёт сразу).
    private func startSpeechIfAllowed() {
        if speechAllowed == nil {
            switch SFSpeechRecognizer.authorizationStatus() {
            case .authorized:
                speechAllowed = true
            case .notDetermined:
                SFSpeechRecognizer.requestAuthorization { [weak self] status in
                    Task { @MainActor in self?.speechAllowed = status == .authorized }
                }
                return
            default:
                speechAllowed = false
            }
        }
        guard speechAllowed == true else { return }
        guard let recognizer = SFSpeechRecognizer(locale: Locale(identifier: "ru-RU")),
              recognizer.isAvailable else { return }
        speechRecognizer = recognizer
        finalizedTranscript = ""
        speechSegmentFailures = 0
        startSpeechSegment()
    }

    /// Один сегмент распознавания. iOS финализирует сегмент после паузы в
    /// речи — тогда его текст дописывается в `finalizedTranscript`, и, пока
    /// запись идёт, стартует следующий сегмент (запрос подменяется в `speechFeed`).
    private func startSpeechSegment() {
        guard let recognizer = speechRecognizer else { return }
        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        // Он-девайс, где доступен: без задержек сети и лимитов Apple.
        if recognizer.supportsOnDeviceRecognition {
            request.requiresOnDeviceRecognition = true
        }
        speechFeed.request = request
        speechTask = recognizer.recognitionTask(with: request) { [weak self] result, error in
            guard let result else {
                // Ошибка сегмента (таймаут демона, отвалилась он-девайс модель):
                // без перезапуска новый сегмент не стартует и живой транскрипт
                // замирает, хотя оверлей продолжает звать «Говорите…».
                guard error != nil else { return }
                Task { @MainActor in
                    guard let self, self.isRecording else { return }
                    guard self.speechSegmentFailures < Self.maxSpeechSegmentFailures else {
                        // Распознавание стабильно не работает — молча живём без
                        // транскрипта: аудио пишется независимо от Speech.
                        self.stopSpeech()
                        return
                    }
                    self.speechSegmentFailures += 1
                    self.startSpeechSegment()
                }
                return
            }
            let text = result.bestTranscription.formattedString
            let isFinal = result.isFinal
            Task { @MainActor in
                guard let self, self.isRecording else { return }
                self.transcript = Self.joinSegments(self.finalizedTranscript, text)
                if isFinal {
                    self.finalizedTranscript = Self.joinSegments(self.finalizedTranscript, text)
                    self.startSpeechSegment()
                }
            }
        }
    }

    private func stopSpeech() {
        speechFeed.request?.endAudio()
        speechFeed.request = nil
        speechTask?.cancel()
        speechTask = nil
        speechRecognizer = nil
    }

    /// Склейка сегментов транскрипта через пробел (пустые не мусорят).
    private static func joinSegments(_ head: String, _ tail: String) -> String {
        if head.isEmpty { return tail }
        if tail.isEmpty { return head }
        return head + " " + tail
    }

    // MARK: WAV

    /// Оборачивает сырой PCM (16 бит LE, моно) в WAV-контейнер.
    private static func wav(pcm: Data, sampleRate: UInt32) -> Data {
        let byteRate = sampleRate * 2
        var header = Data(capacity: 44)
        header.append(contentsOf: Array("RIFF".utf8))
        header.append(le32(UInt32(36 + pcm.count)))
        header.append(contentsOf: Array("WAVE".utf8))
        header.append(contentsOf: Array("fmt ".utf8))
        header.append(le32(16))                    // размер fmt-чанка
        header.append(le16(1))                     // PCM
        header.append(le16(1))                     // моно
        header.append(le32(sampleRate))
        header.append(le32(byteRate))
        header.append(le16(2))                     // block align
        header.append(le16(16))                    // бит на сэмпл
        header.append(contentsOf: Array("data".utf8))
        header.append(le32(UInt32(pcm.count)))
        return header + pcm
    }

    private static func le16(_ v: UInt16) -> Data {
        withUnsafeBytes(of: v.littleEndian) { Data($0) }
    }

    private static func le32(_ v: UInt32) -> Data {
        withUnsafeBytes(of: v.littleEndian) { Data($0) }
    }
}

/// Потокобезопасная точка подачи аудио-буферов в Speech: tap захватывает её
/// один раз, текущий запрос внутри подменяется при перезапуске сегмента.
private final class SpeechFeed: @unchecked Sendable {
    private let lock = NSLock()
    private var current: SFSpeechAudioBufferRecognitionRequest?

    var request: SFSpeechAudioBufferRecognitionRequest? {
        get { lock.lock(); defer { lock.unlock() }; return current }
        set { lock.lock(); current = newValue; lock.unlock() }
    }

    func append(_ buffer: AVAudioPCMBuffer) {
        lock.lock()
        let req = current
        lock.unlock()
        req?.append(buffer)
    }
}

/// Держатель подписок NotificationCenter: снимает их при разрушении
/// владельца (deinit @MainActor-класса до его свойств не дотянется).
private final class ObserverBox {
    var tokens: [NSObjectProtocol] = []

    deinit {
        let center = NotificationCenter.default
        for token in tokens {
            center.removeObserver(token)
        }
    }
}

/// Обёртка движка для переноса между потоками (AVAudioEngine не Sendable,
/// но используется последовательно: собрали в фоне — владеет главный).
private final class EngineBox: @unchecked Sendable {
    let engine: AVAudioEngine
    init(engine: AVAudioEngine) { self.engine = engine }
}

/// Потокобезопасный накопитель PCM: tap движка пишет с аудио-потока,
/// `drain()` читается с главного. Кап ~2.8 МБ (~87 сек) держит WAV под
/// серверным лимитом 3 МБ — более длинная надиктовка вырождена.
private final class PCMSink: @unchecked Sendable {
    private let lock = NSLock()
    private var data = Data()
    private let cap = 2_800_000
    /// Колбэк уровня громкости (зовётся с аудио-потока, троттлится по буферам).
    private var _onLevel: (@Sendable (Float) -> Void)?

    var onLevel: (@Sendable (Float) -> Void)? {
        get { lock.lock(); defer { lock.unlock() }; return _onLevel }
        set { lock.lock(); _onLevel = newValue; lock.unlock() }
    }

    func publishLevel(_ level: Float) {
        onLevel?(level)
    }

    func append(_ chunk: Data) {
        lock.lock()
        defer { lock.unlock() }
        guard data.count < cap else { return }
        data.append(chunk)
    }

    func drain() -> Data {
        lock.lock()
        defer { lock.unlock() }
        return data
    }

    func reset() {
        lock.lock()
        data = Data()
        lock.unlock()
    }
}

/// Ошибки записи голоса — текст показывается пользователю.
enum AudioRecorderError: LocalizedError {
    case failedToStart
    /// Предыдущий старт ещё поднимает движок (двойное нажатие).
    case alreadyStarting

    var errorDescription: String? {
        switch self {
        case .failedToStart:
            return "Не удалось начать запись. Проверьте доступ к микрофону"
        case .alreadyStarting:
            return "Запись ещё готовится. Попробуйте ещё раз"
        }
    }
}
