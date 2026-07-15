import AVFoundation
import Foundation
import Observation

/// Запись голосового ввода расхода (hold-to-talk) через `AVAudioRecorder`.
///
/// Формат — AAC в ADTS-контейнере (`.aac` + `kAudioFormatMPEG4AAC`), mime
/// `audio/aac`. Это важно: серверный allowlist принимает `audio/aac`, но НЕ
/// `audio/mp4`/`m4a`, поэтому пишем именно в `.aac`, а не в `.m4a`. Битрейт
/// ~16 kbps mono — достаточно для распознавания речи и держит размер части под
/// серверным лимитом (audio ≤3 МБ).
@MainActor
@Observable
final class AudioRecorder {
    /// true — идёт запись (для подсветки кнопки-микрофона).
    private(set) var isRecording = false
    /// Данные последней записи (mime `audio/aac`) — их шлёт
    /// `APIClient.parseOperation(audio:)`. nil до первой записи/после сброса.
    private(set) var audioData: Data?

    /// MIME записанного аудио — фиксированный `audio/aac` (см. заметку о формате).
    let mimeType = "audio/aac"

    private var recorder: AVAudioRecorder?
    private var fileURL: URL?

    /// Запрашивает доступ к микрофону. Возвращает true, если доступ выдан.
    /// Вызывать перед первым `start()`; системный prompt показывается один раз.
    func requestPermission() async -> Bool {
        await withCheckedContinuation { continuation in
            AVAudioApplication.requestRecordPermission { granted in
                continuation.resume(returning: granted)
            }
        }
    }

    /// Начинает запись (hold-to-talk: вызывать по нажатию). Настраивает
    /// аудиосессию на запись и стартует `AVAudioRecorder` в AAC-файл во временной
    /// директории. Бросает, если сессия/рекордер не поднялись.
    func start() throws {
        guard !isRecording else { return }
        audioData = nil

        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.record, mode: .default)
        try session.setActive(true)

        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("splitty-voice-\(UUID().uuidString).aac")
        // kAudioFormatMPEG4AAC + расширение .aac → ADTS-контейнер (audio/aac),
        // а не MP4/m4a — иначе серверный allowlist отвергнет часть.
        let settings: [String: Any] = [
            AVFormatIDKey: Int(kAudioFormatMPEG4AAC),
            AVSampleRateKey: 16_000,
            AVNumberOfChannelsKey: 1,
            AVEncoderBitRateKey: 16_000,
            AVEncoderAudioQualityKey: AVAudioQuality.medium.rawValue,
        ]
        let recorder = try AVAudioRecorder(url: url, settings: settings)
        guard recorder.record() else {
            throw AudioRecorderError.failedToStart
        }
        self.recorder = recorder
        self.fileURL = url
        isRecording = true
    }

    /// Останавливает запись (отпустили микрофон) и загружает результат в
    /// `audioData`. Деактивирует аудиосессию. Возвращает записанные данные или
    /// nil, если запись не велась/файл пуст.
    @discardableResult
    func stop() -> Data? {
        guard isRecording, let recorder else { return nil }
        recorder.stop()
        isRecording = false
        self.recorder = nil

        let session = AVAudioSession.sharedInstance()
        try? session.setActive(false, options: .notifyOthersOnDeactivation)

        defer {
            if let fileURL { try? FileManager.default.removeItem(at: fileURL) }
            fileURL = nil
        }
        guard let fileURL,
              let data = try? Data(contentsOf: fileURL), !data.isEmpty else {
            return nil
        }
        audioData = data
        return data
    }

    /// Сбрасывает записанное аудио (после успешной отправки/отмены).
    func reset() {
        audioData = nil
    }
}

/// Ошибки записи голоса — текст показывается пользователю (Russian).
enum AudioRecorderError: LocalizedError {
    case failedToStart

    var errorDescription: String? {
        switch self {
        case .failedToStart:
            return "Не удалось начать запись. Проверьте доступ к микрофону"
        }
    }
}
