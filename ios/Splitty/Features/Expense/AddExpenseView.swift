import os
import PhotosUI
import SwiftUI
import UIKit

/// Добавление/редактирование расхода (премиум-стиль): секции-карточки
/// на нейтральном фоне — выбор группы чипами, описание + крупная сумма
/// по центру, строка «Заплатили вы и разделено поровну» с чипами выбора,
/// CTA «Сохранить» — pill внизу экрана.
struct AddExpenseView: View {
    private let roomId: String?
    private let editOperation: Operation?
    /// Локальная (неотправленная) запись outbox для правки; сохранение
    /// меняет саму запись, сеть не нужна.
    private let editEntry: OutboxEntry?
    private let onDone: (() -> Void)?

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var model = AddExpenseViewModel()
    @State private var isPayerPickerPresented = false
    @State private var isSplitPickerPresented = false
    @State private var isDeleteLocalConfirmPresented = false
    @FocusState private var focusedField: Field?

    // MARK: AI-композер (голос + фото чека)

    /// Запись голоса (hold-to-talk) и съёмка/выбор фото чека — их данные
    /// уходят в `model.parse(...)`. @State: живут вместе с экраном.
    @State private var recorder = AudioRecorder()
    @State private var capture = ReceiptCapture()
    /// Кэш результата запроса доступа к микрофону (первый prompt — на .task).
    @State private var micGranted = false
    /// Выбор источника фото и презентации пикеров/камеры.
    @State private var isPhotoSourceDialogPresented = false
    @State private var isCameraPresented = false
    @State private var photoItem: PhotosPickerItem?
    @State private var isPhotoPickerPresented = false
    /// Открытая на правку позиция чека (шит) и нераспознанное имя на сопоставление.
    @State private var itemEditTarget: ItemEditTarget?
    @State private var unknownTarget: UnknownTarget?
    /// Пользователь выбрал ручной ввод (скрывает AI-композер, показывает поля).
    @State private var manualMode = false
    /// Палец сейчас на микрофоне (hold-to-talk). Закрывает гонку короткого
    /// тапа: старт записи асинхронный (доступ + аудиосессия), и «отпустили»
    /// может прийти РАНЬШЕ, чем запись началась — тогда стартовать нельзя,
    /// иначе запись зависнет включённой навсегда.
    @State private var isMicPressed = false
    /// Палец увели влево за порог отмены (как в Telegram): отпускание
    /// не отправит запись, оверлей показывает состояние «отмена».
    @State private var isCancellingRecording = false
    /// Запись «защёлкнута» свайпом вверх (как замок в Telegram): палец можно
    /// убрать, запись продолжается до «Готово»/«Отмена» на оверлее.
    @State private var isRecordingLocked = false
    /// Текущее смещение пальца при записи — микрофон в оверлее едет за пальцем
    /// (вверх — к замку, влево — к отмене).
    @State private var recordingDragOffset: CGSize = .zero
    /// Триггер встряски поля «выберите группу»: тап по микрофону без выбранной
    /// группы подсвечивает, ЧТО нужно заполнить, вместо мёртвой кнопки.
    @State private var groupNudge = 0
    /// «Отмена» на оверлее распознавания появляется с задержкой (~2.5 с).
    @State private var showParsingCancel = false
    /// Отказ в доступе к микрофону: алерт с кнопкой «Открыть Настройки».
    @State private var isMicPermissionAlertPresented = false
    /// Фактический фрейм кнопки-микрофона на экране (global): оверлей рисует
    /// свой микрофон РОВНО в этой точке и того же размера — кнопка «продолжается».
    @State private var micButtonFrame: CGRect = .zero
    /// Последняя надиктовка: живёт в форме, чтобы фото чека, добавленное СЛЕДОМ,
    /// ушло в Gemini вместе с голосом одним запросом — так модель сопоставляет
    /// цены и распределение напрямую.
    @State private var lastAudio: Data?
    /// Экран «записано, распознавание не началось»: выбор фото/распознать/отмена.
    @State private var isReviewPresented = false

    /// Пустая форма без выбора ручного режима — показываем ТОЛЬКО AI-композер,
    /// чтобы ручные поля не мешались с распознаванием.
    private var showComposer: Bool { model.isEmptyForm && !manualMode }

    /// Открытая на правку позиция чека (Identifiable-обёртка индекса для `.sheet(item:)`).
    private struct ItemEditTarget: Identifiable {
        let id = UUID()
        let index: Int
    }

    /// Нераспознанное имя позиции на сопоставление участнику.
    private struct UnknownTarget: Identifiable {
        let id = UUID()
        let itemIndex: Int
        let name: String
    }

    private enum Field: Hashable {
        case description
        case sum
        /// Поле точной доли участника (режим «По суммам»), ключ — user id.
        case amount(Int)
    }

    init(
        roomId: String? = nil,
        editOperation: Operation? = nil,
        editEntry: OutboxEntry? = nil,
        onDone: (() -> Void)? = nil
    ) {
        self.roomId = roomId
        self.editOperation = editOperation
        self.editEntry = editEntry
        self.onDone = onDone
    }

    var body: some View {
        let recActive = isMicPressed || recorder.isRecording
        return ZStack {
            navigation
            // оверлеи — поверх ВСЕГО (навбар, нижняя панель), чтобы запись и
            // распознавание читались как полноэкранный процесс
            if !recActive, isReviewPresented {
                reviewOverlay.zIndex(1)
            } else if !recActive, model.isParsing {
                parsingOverlay.zIndex(1)
            }
            // Оверлей записи смонтирован ПОСТОЯННО (в покое — opacity 0 и
            // прозрачен для касаний): блюр и вью-дерево уже построены, нажатие
            // лишь проявляет их. Монтирование с нуля (UIVisualEffectView,
            // GeometryReader, маски) в момент касания и было задержкой отклика.
            RecordingOverlay(
                isActive: recActive,
                transcript: recorder.transcript,
                isCancelling: isCancellingRecording,
                isLocked: isRecordingLocked,
                isPreparing: !recorder.isRecording,
                drag: recordingDragOffset,
                startedAt: recorder.startedAt,
                level: CGFloat(recorder.level),
                micFrame: micButtonFrame,
                hints: model.missingInfoHints,
                onStop: {
                    isRecordingLocked = false
                    stopRecordingAndParse()
                },
                onCancel: {
                    isRecordingLocked = false
                    cancelRecording()
                }
            )
            // Кнопки «Готово»/«Отмена» кликабельны только в закреплённом
            // режиме — пока палец на микрофоне (и в покое) оверлей прозрачен
            // для касаний. Проверка recActive обязательна: замок защёлкивается
            // ДО того, как поднялся движок (isMicPressed при этом снимается), и
            // невидимый (opacity 0) оверлей всё равно ловил касания — SwiftUI
            // хит-тестит прозрачные вью, и форма замирала намертво.
            .allowsHitTesting(isRecordingLocked && recActive)
            .zIndex(2)
            if let toast = model.toastMessage {
                toastView(toast).zIndex(3)
            }
        }
        .animation(.spring(duration: 0.3), value: model.toastMessage)
        .animation(.spring(duration: 0.25), value: isReviewPresented)
        // Звонок/сброс медиасервисов обрывают запись мимо жестов: в закреплённом
        // режиме пальца нет, cancelTracking не придёт — снимаем замок сами,
        // иначе оверлей «Запись идёт» висит над остановленным движком.
        .onAppear {
            recorder.onInterrupted = {
                isRecordingLocked = false
                isMicPressed = false
                isCancellingRecording = false
                recordingDragOffset = .zero
                model.toastMessage = String(localized: "Запись прервана системой. Продиктуйте ещё раз")
            }
        }
        // Второй хептик — в момент, когда запись РЕАЛЬНО пошла (движок поднялся):
        // ощущение «заработало», даже если подготовка заняла долю секунды.
        .onChange(of: recorder.isRecording) { _, isOn in
            if isOn { Haptics.tap() }
        }
        // Лимит надиктовки — минута (кольцо-прогресс вокруг микрофона).
        // На исходе — автостоп с распознаванием того, что успели сказать.
        .task(id: recorder.isRecording) {
            guard recorder.isRecording else { return }
            try? await Task.sleep(for: .seconds(60))
            guard recorder.isRecording else { return }
            isRecordingLocked = false
            isMicPressed = false
            model.toastMessage = String(localized: "Минута — лимит записи. Распознаю, что успели сказать")
            stopRecordingAndParse()
        }
    }

    /// Экран «записано/снято, распознавание ЕЩЁ НЕ началось»: явный выбор —
    /// добавить второй источник (он уйдёт вместе с первым одним запросом) или
    /// сразу «Распознать». Транскрипт здесь НЕ показываем: локальное
    /// распознавание может отличаться от того, что поймёт Gemini, и смущает
    /// как «вот что записалось» — только нейтральная длительность.
    /// Показывается только на ПЕРВОМ вводе в пустую форму (см. `stopsAtReview`).
    /// Длительность последней записи в секундах (WAV 16 кГц/16 бит ≈ 32 КБ/с).
    private var recordedSeconds: Int {
        max(1, (lastAudio?.count ?? 0) / 32_000)
    }

    /// Экран разбора открыт по фото чека, а не по диктовке: одна вёрстка, но
    /// зеркальные тексты и вторичное действие.
    private var reviewIsPhoto: Bool { lastAudio == nil }

    private var reviewOverlay: some View {
        ZStack {
            Rectangle()
                .fill(.ultraThinMaterial)
                .environment(\.colorScheme, .dark)
                .ignoresSafeArea()
            Color.black.opacity(0.35).ignoresSafeArea()

            VStack(spacing: 0) {
                Spacer(minLength: 40)
                HStack(spacing: 8) {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 18))
                        .foregroundStyle(Color.accent)
                    Text(reviewIsPhoto ? "Чек снят" : "Записано")
                        .scaledFont(size: 20, weight: .bold)
                        .foregroundStyle(.white)
                }
                Text(reviewIsPhoto ? "Фото чека" : "Голосовая запись · \(recordedSeconds) сек")
                    .scaledFont(size: 15, weight: .medium)
                    .foregroundStyle(.white.opacity(0.75))
                    .padding(.top, 12)
                Spacer(minLength: 24)

                // Иерархия: главное действие — «Распознать» (основной путь),
                // второй источник — вторичный усилитель, отмена — тихая третья.
                VStack(spacing: 12) {
                    Button {
                        sendParse(image: capture.imageData)
                    } label: {
                        Text("Распознать")
                    }
                    .buttonStyle(.primaryPill)

                    Button {
                        if reviewIsPhoto {
                            startVoiceFromReview()
                        } else {
                            isPhotoSourceDialogPresented = true
                        }
                    } label: {
                        VStack(spacing: 3) {
                            Label(
                                reviewIsPhoto ? "Добавить голосом" : "Добавить фото чека",
                                systemImage: reviewIsPhoto ? "mic.fill" : "camera.fill"
                            )
                            .scaledFont(size: 15, weight: .semibold)
                            .foregroundStyle(.white)
                            Text(reviewIsPhoto
                                ? "скажите, кто что взял — точнее"
                                : "цены возьмём с чека — точнее")
                                .scaledFont(size: 12, relativeTo: .footnote)
                                .foregroundStyle(.white.opacity(0.65))
                        }
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 12)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)

                    Button {
                        isReviewPresented = false
                        if reviewIsPhoto {
                            capture.reset()
                        } else {
                            lastAudio = nil
                            recorder.reset()
                        }
                    } label: {
                        Text(reviewIsPhoto ? "Убрать фото" : "Отменить запись")
                            .scaledFont(size: 14, weight: .medium)
                            .foregroundStyle(.white.opacity(0.6))
                            .padding(.vertical, 8)
                    }
                    .buttonStyle(.plain)
                }
                .padding(.horizontal, 28)
                .padding(.bottom, 40)
            }
            .padding(.horizontal, 24)
        }
        .transition(.opacity)
    }

    /// Тост-подтверждение внизу экрана («Саня — это Александр. Запомнил»);
    /// гаснет сам через пару секунд.
    private func toastView(_ text: String) -> some View {
        VStack {
            Spacer()
            HStack(spacing: 10) {
                Image(systemName: "checkmark.circle.fill")
                    .font(.system(size: 16))
                    .foregroundStyle(Color.accent)
                Text(text)
                    .scaledFont(size: 14, weight: .semibold)
                    .foregroundStyle(Color.bg)
                    .multilineTextAlignment(.leading)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 13)
            .background(Color.ink, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
            .shadow(color: Color.black.opacity(0.25), radius: 16, y: 6)
            .padding(.horizontal, 20)
            // На композере нижняя панель выше (микрофон 96pt + подпись) —
            // тост не должен ложиться на сам микрофон.
            .padding(.bottom, showComposer ? 158 : 90)
        }
        .transition(.opacity.combined(with: .move(edge: .bottom)))
        .allowsHitTesting(false)
        .task(id: text) {
            try? await Task.sleep(for: .seconds(2.8))
            // Отменённый таймер (текст сменился/тост скрыт) не должен гасить
            // уже ДРУГОЙ тост: sleep при отмене возвращается мгновенно.
            guard !Task.isCancelled else { return }
            model.toastMessage = nil
        }
    }

    private var navigation: some View {
        NavigationStack {
            content
                .navigationTitle(
                    editOperation == nil && editEntry == nil ? "Добавить расход" : "Изменить расход"
                )
                .navigationBarTitleDisplayMode(.inline)
                .background(Color.bg)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Отмена") { dismiss() }
                    }
                }
                .task {
                    await model.load(
                        repo: session.repo,
                        fixedRoomId: roomId,
                        editOperation: editOperation,
                        editEntry: editEntry,
                        me: session.me
                    )
                    // Автофокус — только в ручной форме (правка операции):
                    // на AI-композере фокус не ставим, иначе после распознавания
                    // «вспоминается» поле описания и выезжает клавиатура.
                    if focusedField == nil, !showComposer {
                        focusedField = .description
                    }
                }
                .sheet(isPresented: $isPayerPickerPresented) {
                    PayerPickerView(model: model, meId: session.me?.id)
                }
                .sheet(isPresented: $isSplitPickerPresented) {
                    SplitPickerView(model: model, meId: session.me?.id)
                }
                .sheet(item: $itemEditTarget) { target in
                    ItemSheetView(model: model, index: target.index, meId: session.me?.id)
                }
                .sheet(item: $unknownTarget) { target in
                    UnknownPickerView(
                        model: model,
                        itemIndex: target.itemIndex,
                        name: target.name,
                        api: session.api
                    )
                }
                .photosPicker(isPresented: $isPhotoPickerPresented, selection: $photoItem, matching: .images)
                .onChange(of: photoItem) { _, newItem in
                    guard newItem != nil else { return }
                    Task {
                        let ok = await capture.load(from: newItem)
                        photoItem = nil
                        guard ok, let data = capture.imageData else { return }
                        handleCaptured(image: data)
                    }
                }
                .fullScreenCover(isPresented: $isCameraPresented) {
                    CameraPicker { image in
                        guard capture.setImage(image), let data = capture.imageData else { return }
                        handleCaptured(image: data)
                    }
                    .ignoresSafeArea()
                }
                .confirmationDialog(
                    "Фото чека",
                    isPresented: $isPhotoSourceDialogPresented,
                    titleVisibility: .visible
                ) {
                    if UIImagePickerController.isSourceTypeAvailable(.camera) {
                        Button("Камера") { isCameraPresented = true }
                    }
                    Button("Галерея") { isPhotoPickerPresented = true }
                    Button("Отмена", role: .cancel) {}
                }
                .confirmationDialog(
                    "Удалить неотправленный расход?",
                    isPresented: $isDeleteLocalConfirmPresented,
                    titleVisibility: .visible
                ) {
                    Button("Удалить", role: .destructive) {
                        deleteLocalEntry()
                    }
                    Button("Отмена", role: .cancel) {}
                } message: {
                    Text("Запись хранится только на этом устройстве и ещё не отправлена на сервер.")
                }
                .alert("Ошибка", isPresented: alertPresented) {
                    Button("ОК", role: .cancel) {}
                } message: {
                    Text(model.alertMessage ?? "")
                }
                // Ошибка распознавания: запись сохранена (lastAudio) —
                // «Повторить» отправляет её же, диктовка не теряется.
                .alert("Не удалось распознать", isPresented: parseRetryPresented) {
                    Button("Повторить") {
                        sendParse(image: capture.imageData)
                    }
                    Button("Отмена", role: .cancel) {}
                } message: {
                    Text(model.parseRetryMessage ?? "")
                }
                // Нет доступа к микрофону: ведём прямо в Настройки, а не
                // объясняем маршрут словами.
                .alert("Нет доступа к микрофону", isPresented: $isMicPermissionAlertPresented) {
                    Button("Открыть Настройки") {
                        if let url = URL(string: UIApplication.openSettingsURLString) {
                            UIApplication.shared.open(url)
                        }
                    }
                    Button("Отмена", role: .cancel) {}
                } message: {
                    Text("Разрешите доступ в Настройках, чтобы диктовать расход голосом")
                }
        }
        .tint(Color.accent)
    }

    /// Удаление локальной записи outbox (форма правки неотправленного расхода).
    private func deleteLocalEntry() {
        guard let editEntry else { return }
        session.outbox.remove(localId: editEntry.localId)
        Haptics.success()
        onDone?()
        dismiss()
    }

    private var alertPresented: Binding<Bool> {
        Binding(
            get: { model.alertMessage != nil },
            set: { if !$0 { model.alertMessage = nil } }
        )
    }

    private var parseRetryPresented: Binding<Bool> {
        Binding(
            get: { model.parseRetryMessage != nil },
            set: { if !$0 { model.parseRetryMessage = nil } }
        )
    }

    @ViewBuilder
    private var content: some View {
        switch model.state {
        case .loading:
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let message):
            ContentUnavailableView {
                Label("Не удалось загрузить", systemImage: "wifi.exclamationmark")
            } description: {
                Text(message)
            } actions: {
                Button("Повторить") {
                    Task {
                        await model.retry(
                            repo: session.repo,
                            fixedRoomId: roomId,
                            editOperation: editOperation,
                            editEntry: editEntry,
                            me: session.me
                        )
                    }
                }
            }
        case .loaded:
            // Панель с микрофоном — ВНЕ ScrollView: внутри safeAreaInset скролл
            // арбитрирует касания (delaysContentTouches) и жест микрофона
            // срабатывает с задержкой ~100-150 мс.
            VStack(spacing: 0) {
                form
                bottomBar
            }
        }
    }

    /// Офлайн-редактирование синхронизированной операции запрещено
    /// (правится только запись outbox — см. дизайн офлайн-режима v1).
    private var isOfflineBlocked: Bool {
        model.isSaveBlocked(isOnline: session.isOnline)
    }

    private var form: some View {
        @Bindable var model = model
        return ScrollView {
            VStack(spacing: 20) {
                if isOfflineBlocked {
                    offlineBlockedNotice
                }
                if roomId == nil {
                    groupPicker
                }
                if showComposer {
                    composerCard
                    parseQuestionLabels
                    manualEntryLink
                } else {
                    if model.canUndoParse {
                        correctionBanner
                    } else if model.didRecognize, !model.hasDraftItems {
                        recognizedBanner
                    }
                    expenseCard(description: $model.descriptionText, sum: $model.sumText)
                    if model.showsPayerLine {
                        payerLineCard
                    }
                    if model.showsSplitCard {
                        parseQuestionLabels
                        splitCard
                    } else {
                        receiptSection
                    }
                    // Из ручного режима можно вернуться к голосу, пока форма
                    // пуста (раньше выход был только закрытием всей формы).
                    if manualMode, model.isEmptyForm {
                        backToVoiceLink
                    }
                }
                if model.isEditingLocalEntry {
                    deleteLocalButton
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 16)
            .padding(.bottom, 24)
        }
        .scrollDismissesKeyboard(.interactively)
    }

    /// Плашка «распознано голосом» для ПЛОСКОГО AI-результата (без позиций):
    /// чтобы такой экран не читался как обычный ручной ввод. Справа — камера:
    /// фото чека уточнит распознанное (цены с чека, распределение из голоса).
    private var recognizedBanner: some View {
        HStack(spacing: 10) {
            Image(systemName: "waveform")
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(Color.accent)
            VStack(alignment: .leading, spacing: 2) {
                Text("Распознано голосом")
                    .scaledFont(size: 14, weight: .semibold)
                    .foregroundStyle(Color.ink)
                Text("Не то? Зажмите микрофон внизу или добавьте фото чека")
                    .scaledFont(size: 12, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
            }
            Spacer(minLength: 8)
            Button {
                if aiDisabled {
                    nudgeAIUnavailable()
                } else {
                    isPhotoSourceDialogPresented = true
                }
            } label: {
                Image(systemName: "camera.fill")
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Color.accent)
                    .frame(width: 38, height: 38)
                    .background(Color.accent.opacity(0.12), in: Circle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Добавить фото чека")
        }
        .padding(14)
        .background(Color.accent.opacity(0.1), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    /// Уточняющие вопросы модели («Сколько стоила пицца?») — видимы в любом
    /// состоянии формы, не только под чеком: без них непонятно, что переспросить.
    @ViewBuilder
    private var parseQuestionLabels: some View {
        if !model.parseQuestions.isEmpty {
            VStack(spacing: 8) {
                ForEach(model.parseQuestions, id: \.self) { question in
                    Label(question, systemImage: "questionmark.circle")
                        .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                        .foregroundStyle(Color.inkSecondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
    }

    /// Ссылка «ввести вручную» под AI-композером: скрывает распознавание и
    /// раскрывает обычные поля (описание/сумма/деление).
    private var manualEntryLink: some View {
        Button {
            withAnimation(.easeInOut(duration: 0.2)) { manualMode = true }
            focusedField = .description
        } label: {
            Text("Ввести вручную")
                .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                .foregroundStyle(Color.inkSecondary)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 6)
        }
        .buttonStyle(.plain)
    }

    /// Обратный путь из ручного режима к AI-композеру (пока форма пуста).
    private var backToVoiceLink: some View {
        Button {
            focusedField = nil
            withAnimation(.easeInOut(duration: 0.2)) { manualMode = false }
        } label: {
            Label("Заполнить голосом", systemImage: "waveform")
                .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                .foregroundStyle(Color.accent)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 6)
        }
        .buttonStyle(.plain)
    }

    /// Полноэкранный оверлей распознавания: тёмный фон + спиннер. Через пару
    /// секунд появляется «Отмена» — зависший запрос не должен запирать
    /// пользователя на экране (запись сохранена, повтор ничего не теряет).
    private var parsingOverlay: some View {
        ZStack {
            Color.black.opacity(0.55).ignoresSafeArea()
            VStack(spacing: 18) {
                ProgressView()
                    .controlSize(.large)
                    .tint(.white)
                Text("Распознаю…")
                    .scaledFont(size: 17, weight: .semibold)
                    .foregroundStyle(.white)
                Text("Считываю расход и раскладываю по позициям")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(.white.opacity(0.7))
                    .multilineTextAlignment(.center)
                    .padding(.horizontal, 40)
                if showParsingCancel {
                    Button("Отмена") {
                        model.cancelParse()
                    }
                    .scaledFont(size: 15, weight: .semibold)
                    .foregroundStyle(.white.opacity(0.9))
                    .padding(.top, 8)
                    .transition(.opacity)
                }
            }
        }
        .transition(.opacity)
        .task {
            // Кнопка не сразу: обычный ответ приходит за 1-2 с, и мигающая
            // «Отмена» только отвлекала бы. Появляется, когда ждать надоело.
            try? await Task.sleep(for: .seconds(2.5))
            guard !Task.isCancelled else { return }
            withAnimation(.easeOut(duration: 0.2)) { showParsingCancel = true }
        }
        .onDisappear { showParsingCancel = false }
    }

    /// Плашка «Нет соединения…»: синхронизированную операцию офлайн
    /// не редактируем (кнопка «Сохранить» заблокирована).
    private var offlineBlockedNotice: some View {
        HStack(spacing: 10) {
            Image(systemName: "wifi.slash")
                .font(.system(size: 16))
                .foregroundStyle(Color.inkSecondary)
            Text("Нет соединения. Можно редактировать только неотправленные операции")
                .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                .foregroundStyle(Color.inkSecondary)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .surfaceCard()
    }

    /// Удаление локальной (неотправленной) записи outbox — с подтверждением.
    private var deleteLocalButton: some View {
        Button(role: .destructive) {
            isDeleteLocalConfirmPresented = true
        } label: {
            Label("Удалить", systemImage: "trash")
                .font(.subheadline.weight(.medium))
                .foregroundStyle(Color.negative)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 16)
                .padding(.vertical, 14)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .surfaceCard(padding: 0)
    }

    /// Нижняя панель. Пустая форма (композер): БОЛЬШОЙ hold-to-talk микрофон
    /// внизу, в зоне большого пальца (наверху до него не дотянуться, а свайп
    /// вверх для замка снизу — естественное движение). Заполненная форма:
    /// камера + микрофон правки + CTA «Сохранить».
    private var bottomBar: some View {
        Group {
            if showComposer {
                composerMicBar
            } else {
                HStack(spacing: 11) {
                    photoCorrectionButton
                    voiceCorrectionMic
                    saveButton
                }
            }
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 8)
        .background(Color.bg)
    }

    /// Большой микрофон композера в нижней панели (зона большого пальца).
    private var composerMicBar: some View {
        VStack(spacing: 8) {
            ZStack {
                Circle()
                    .strokeBorder(Color.accent.opacity(0.22), lineWidth: 1.5)
                    .frame(width: 96, height: 96)
                Circle()
                    .fill(LinearGradient(colors: [Color.accent, Color.accentPressed],
                                         startPoint: .topLeading, endPoint: .bottomTrailing))
                    .frame(width: 82, height: 82)
                    .overlay {
                        Image(systemName: "mic.fill")
                            .font(.system(size: 30, weight: .semibold))
                            .foregroundStyle(.white)
                    }
                    .shadow(color: Color.accent.opacity(0.4), radius: 14, x: 0, y: 8)
            }
            .opacity(aiDisabled ? 0.45 : 1)
            // Squash при касании — отклик стартует НА кнопке, ещё до того,
            // как оверлей смонтируется; его микрофон подхватит с того же места.
            .scaleEffect(isMicPressed ? 0.9 : 1)
            .animation(.spring(duration: 0.15), value: isMicPressed)
            .onGeometryChange(for: CGRect.self) { $0.frame(in: .global) } action: { micButtonFrame = $0 }
            .overlay { micTouchSurface }
            .accessibilityLabel("Записать голосом")
            .accessibilityHint("Удерживайте и говорите; свайп вверх — закрепить")
            Text("Удерживайте, чтобы говорить")
                .scaledFont(size: 12, weight: .medium, relativeTo: .footnote)
                .foregroundStyle(Color.inkSecondary)
        }
        .frame(maxWidth: .infinity)
    }

    /// Кнопка «фото чека» в нижней панели: снимок уточнит цены/позиции
    /// текущего черновика (сервер применяет фото К черновику, а не заново).
    private var photoCorrectionButton: some View {
        Button {
            if aiDisabled {
                nudgeAIUnavailable()
            } else {
                isPhotoSourceDialogPresented = true
            }
        } label: {
            ZStack {
                Circle().fill(Color.surface)
                Circle().strokeBorder(Color.accent.opacity(0.4), lineWidth: 1.5)
                Image(systemName: "camera.fill")
                    .font(.system(size: 19, weight: .semibold))
                    .foregroundStyle(Color.accent)
            }
            .frame(width: 54, height: 54)
            .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .opacity(aiDisabled ? 0.45 : 1)
        .accessibilityLabel("Добавить фото чека")
    }

    /// Круглый микрофон голосовой правки (hold-to-talk): скажи, что поправить —
    /// текущий черновик уйдёт на сервер вместе с голосом.
    private var voiceCorrectionMic: some View {
        ZStack {
            Circle()
                .fill(recorder.isRecording ? Color.accent : Color.surface)
            Circle()
                .strokeBorder(Color.accent, lineWidth: 1.5)
            Image(systemName: recorder.isRecording ? "waveform" : "mic.fill")
                .font(.system(size: 21, weight: .semibold))
                .foregroundStyle(recorder.isRecording ? .white : Color.accent)
        }
        .frame(width: 54, height: 54)
        .contentShape(Circle())
        .animation(.spring(duration: 0.2), value: recorder.isRecording)
        .scaleEffect(isMicPressed ? 0.9 : 1)
        .animation(.spring(duration: 0.15), value: isMicPressed)
        .onGeometryChange(for: CGRect.self) { $0.frame(in: .global) } action: { micButtonFrame = $0 }
        .overlay { micTouchSurface }
        .opacity(aiDisabled ? 0.45 : 1)
        .accessibilityLabel("Поправить голосом")
        .accessibilityHint("Удерживайте и скажите, что изменить")
    }

    /// CTA «Сохранить»: premium pill вместо тулбарной кнопки. При блокировке
    /// кнопка живая: тап объясняет причину тостом (как нудж микрофона),
    /// а не молча игнорируется.
    private var saveButton: some View {
        Button {
            if let reason = saveBlockedReason {
                Haptics.warning()
                model.toastMessage = reason
                return
            }
            Task {
                let saved = await model.save(
                    api: session.api,
                    outbox: session.outbox,
                    isOnline: session.isOnline
                )
                if saved {
                    Haptics.success()
                    // Единая инвалидация: все экраны-списки
                    // перезагрузятся по dataVersion.
                    session.noteDataChanged()
                    // Подтолкнуть outbox: исправленная/новая запись уйдёт сразу,
                    // если сеть есть (без сети синк — no-op до триггера).
                    Task { await session.syncOutbox() }
                    onDone?()
                    dismiss()
                }
            }
        } label: {
            if model.isSaving {
                ProgressView()
                    .tint(.white)
            } else {
                Text("Сохранить")
            }
        }
        .buttonStyle(.primaryPill)
        // Блокировка по canSave — НЕ через .disabled: живой тап объясняет
        // причину тостом (см. action). Приглушение — визуальный сигнал.
        .opacity(saveBlockedReason == nil ? 1 : 0.6)
        // Сохранение в полёте и офлайн-правка синхронизированной операции —
        // жёсткий disabled (причину офлайна объясняет плашка сверху).
        .disabled(model.isSaving || isOfflineBlocked)
    }

    /// Причина блокировки «Сохранить» (nil — можно сохранять).
    private var saveBlockedReason: String? { model.saveBlockedReason }

    /// Компактный микрофон в нижней панели (hold-to-talk) — голосовая правка
    /// черновика. Недоступен офлайн (AI требует сети) и во время распознавания.
    // MARK: AI-композер

    /// Композер на пустой форме: подсказка «микрофон внизу» + «Сфотографировать
    /// чек». Сам микрофон — в нижней панели, в зоне большого пальца
    /// (`composerMicBar`). Кнопки требуют сети — офлайн заблокированы.
    private var composerCard: some View {
        VStack(spacing: 16) {
            VStack(spacing: 6) {
                HStack(spacing: 8) {
                    Image(systemName: "waveform")
                        .font(.system(size: 17, weight: .semibold))
                        .foregroundStyle(Color.accent)
                    Text("Надиктуйте расход")
                        .scaledFont(size: 16, weight: .semibold)
                        .foregroundStyle(Color.ink)
                }
                Text("Зажмите микрофон внизу и скажите,\nкто что взял и сколько это стоило")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
                    .multilineTextAlignment(.center)
                    .lineSpacing(2)
            }
            .padding(.top, 4)

            HStack(spacing: 12) {
                Rectangle().fill(Color.hairline).frame(height: 1)
                Text("или")
                    .scaledFont(size: 11, weight: .semibold, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
                    .textCase(.uppercase)
                Rectangle().fill(Color.hairline).frame(height: 1)
            }
            .padding(.top, 2)

            Button {
                // Кнопка живая и при aiDisabled: тап объясняет причину
                // (встряска поля группы/тост), а не игнорируется молча.
                if aiDisabled {
                    nudgeAIUnavailable()
                } else {
                    isPhotoSourceDialogPresented = true
                }
            } label: {
                Label("Сфотографировать чек", systemImage: "camera.fill")
                    .scaledFont(size: 15, weight: .semibold)
                    .foregroundStyle(Color.accent)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 14)
                    .background(Color.accent.opacity(0.12), in: Capsule())
                    .contentShape(Capsule())
            }
            .buttonStyle(.plain)
            .opacity(aiDisabled ? 0.45 : 1)

            if let reason = aiDisabledReason {
                Text(reason)
                    .scaledFont(size: 12, weight: .medium, relativeTo: .footnote)
                    .foregroundStyle(Color.negative.opacity(0.9))
            }
        }
        .frame(maxWidth: .infinity)
        .surfaceCard()
    }

    /// AI-действия недоступны: не выбрана группа, нет сети (распознавание —
    /// только онлайн) или уже идёт запрос.
    private var aiDisabled: Bool {
        model.selectedRoomId == nil || !session.isOnline || model.isParsing
    }

    /// Причина блокировки AI для подсказки под композером (nil — доступно).
    private var aiDisabledReason: String? {
        if model.selectedRoomId == nil { return String(localized: "Сначала выберите группу") }
        if !session.isOnline { return String(localized: "Распознавание доступно только онлайн") }
        return nil
    }

    /// Пороги жестов записи (как в Telegram): вверх — закрепить (палец можно
    /// убрать), влево — отмена.
    private static let lockDragThreshold: CGFloat = -70
    private static let cancelDragThreshold: CGFloat = -70

    /// Замеры отклика на нажатие микрофона: `log stream --predicate
    /// 'subsystem == "com.zagir.splitty"'` или запуск с консолью devicectl.
    private static let latencyLog = Logger(subsystem: "com.zagir.splitty", category: "latency")

    /// Hold-to-talk через UIKit-поверхность (`MicTouchSurface`): нажали —
    /// запись, отпустили — распознавание. Свайп ВВЕРХ защёлкивает запись
    /// (замок, как в Telegram) — дальше кнопки «Готово»/«Отмена» на оверлее;
    /// свайп ВЛЕВО — отмена. SwiftUI DragGesture здесь не годится: его
    /// арбитраж задерживал первый onChanged на ~100-300 мс.
    private var micTouchSurface: some View {
        // Поверхность активна и при aiDisabled: тап по «выключенному»
        // микрофону объясняет причину (встряска поля группы/тост), а не
        // молча съедает касание.
        MicTouchSurface(
            onBegan: handleMicTouchBegan,
            onMoved: handleMicTouchMoved,
            onEnded: handleMicTouchEnded,
            onCancelled: handleMicTouchCancelled
        )
    }

    /// Текст нудж-тоста «выберите группу»: константа, чтобы выбор группы
    /// мог мгновенно погасить именно этот тост (см. `groupChip`).
    private static var selectGroupToast: String { String(localized: "Сначала выберите группу") }

    /// AI недоступен, но пользователь жмёт микрофон: показываем, ЧТО не так —
    /// встряхиваем поле выбора группы или объясняем тостом (офлайн).
    private func nudgeAIUnavailable() {
        Haptics.warning()
        if model.selectedRoomId == nil {
            groupNudge += 1
            model.toastMessage = Self.selectGroupToast
        } else if let reason = aiDisabledReason {
            model.toastMessage = reason
        }
    }

    private func handleMicTouchBegan(_ sample: MicTouchSurface.Sample) {
        guard !isMicPressed else { return }
        if aiDisabled {
            nudgeAIUnavailable()
            return
        }
        // VoiceOver/Switch Control: «удерживайте» невыполнимо стандартными
        // жестами — переключаем на тап-режим: тап — запись (сразу закреплена,
        // на экране кнопки «Готово»/«Отмена»), повторный тап не нужен.
        if UIAccessibility.isVoiceOverRunning {
            guard !isRecordingLocked, !recorder.isRecording else { return }
            isRecordingLocked = true
            Haptics.tap()
            startRecordingIfNeeded()
            return
        }
        guard !isRecordingLocked else { return }
        // Латентность доставки: сколько шло событие от touchesBegan
        // (UITouch.timestamp) до хендлера. Ожидание после UIKit-фикса: 0-16 мс.
        let deliveryMs = Int((ProcessInfo.processInfo.systemUptime - sample.timestamp) * 1000)
        isMicPressed = true
        Haptics.tap() // мгновенный отклик касания — до старта движка
        startRecordingIfNeeded()
        let touchUptime = sample.timestamp
        Task { @MainActor in
            // Следующий тик main runloop ≈ кадр, где оверлей уже виден.
            // Норма после UIKit-фикса: delivery ~25 мс, коммит ~45 мс от касания.
            let totalMs = Int((ProcessInfo.processInfo.systemUptime - touchUptime) * 1000)
            Self.latencyLog.info("mic press: delivery \(deliveryMs)ms, overlay committed +\(totalMs)ms from touch")
        }
    }

    private func handleMicTouchMoved(_ sample: MicTouchSurface.Sample) {
        guard !isRecordingLocked, isMicPressed else { return }
        recordingDragOffset = sample.translation
        let t = sample.translation
        // Замок защёлкивается сразу при пересечении порога (не ждём отпускания).
        if t.height < Self.lockDragThreshold, abs(t.height) >= abs(t.width) {
            isRecordingLocked = true
            isMicPressed = false
            isCancellingRecording = false
            recordingDragOffset = .zero
            Haptics.success()
            return
        }
        let cancelling = t.width < Self.cancelDragThreshold && abs(t.width) > abs(t.height)
        if cancelling != isCancellingRecording {
            isCancellingRecording = cancelling
            Haptics.tap()
        }
    }

    private func handleMicTouchEnded(_ sample: MicTouchSurface.Sample) {
        guard !isRecordingLocked else { return } // палец отпущен после замка
        isMicPressed = false
        let t = sample.translation
        let cancelled = t.width < Self.cancelDragThreshold && abs(t.width) > abs(t.height)
        isCancellingRecording = false
        recordingDragOffset = .zero
        if cancelled {
            cancelRecording()
        } else {
            stopRecordingAndParse()
        }
    }

    /// Система отобрала касание (звонок, сворачивание, чужой распознаватель):
    /// запись выбрасывается — отправлять обрывок опаснее, чем переспросить.
    private func handleMicTouchCancelled() {
        guard !isRecordingLocked else { return }
        isMicPressed = false
        isCancellingRecording = false
        recordingDragOffset = .zero
        cancelRecording()
    }

    /// Отмена записи жестом: стоп без отправки, запись выбрасывается.
    private func cancelRecording() {
        recorder.stop()
        recorder.reset()
    }

    /// Старт записи по нажатию (idempotent): запрашивает доступ к микрофону при
    /// первом использовании, затем поднимает `AVAudioRecorder`. Каждый await —
    /// точка, где палец могли уже отпустить: перепроверяем `isMicPressed`,
    /// иначе запись стартует после «отпустили» и остаётся включённой навсегда.
    private func startRecordingIfNeeded() {
        guard !recorder.isRecording, !model.isParsing else { return }
        Task {
            if !micGranted {
                micGranted = await recorder.ensurePermission()
            }
            guard micGranted else {
                // Замок снимаем обязательно: в VoiceOver-режиме он выставляется
                // ДО запроса доступа, а оверлей с ним ловит касания (прозрачная
                // SwiftUI-вьюха всё равно hit-testable) — форма становилась
                // полностью нежатой, выйти можно было только перезапуском.
                isRecordingLocked = false
                isMicPressed = false
                isMicPermissionAlertPresented = true
                return
            }
            // Палец уже подняли (короткий тап), пока ждали доступ/переключались
            // задачи — записывать нечего, не стартуем. Закреплённый режим
            // (замок/VoiceOver-тап) пишет без прижатого пальца.
            guard isMicPressed || isRecordingLocked, !recorder.isRecording else { return }
            do {
                let engineT0 = Date()
                try await recorder.start()
                Self.latencyLog.info("record: engine up in \(Int(Date().timeIntervalSince(engineT0) * 1000))ms")
                // Отпустили ровно во время подъёма аудиосессии — гасим сразу:
                // столь короткая запись бесполезна, а «вечная» анимация вредна.
                if !isMicPressed, !isRecordingLocked {
                    recorder.stop()
                    recorder.reset()
                }
            } catch {
                // Движок не поднялся — замок тоже снимаем, иначе оверлей
                // остаётся поверх формы и перехватывает касания.
                isRecordingLocked = false
                isMicPressed = false
                model.alertMessage = humanErrorText(error)
            }
        }
    }

    /// Отпустили микрофон — голос сразу уходит на распознавание (никаких
    /// промежуточных диалогов: они читались как «ничего не сохранилось»).
    /// Если к форме уже приложено фото чека, оно уходит ВМЕСТЕ с голосом:
    /// в одном запросе Gemini сопоставляет цены с фото и распределение из
    /// голоса напрямую — точнее, чем последовательные правки по черновику.
    private func stopRecordingAndParse() {
        guard recorder.isRecording, let data = recorder.stop() else {
            recorder.reset()
            return
        }
        // Тап вместо удержания: ~0.7 сек WAV 16 кГц/16 бит ≈ 24 КБ. Такая
        // «запись» пуста — не гоняем её в Gemini, а подсказываем жест
        // тостом: это обучение, модальный алерт «Ошибка» тут пугал.
        guard data.count >= 24_000 else {
            recorder.reset()
            model.toastMessage = String(localized: "Удерживайте микрофон, пока говорите, и отпустите, когда закончите")
            return
        }
        lastAudio = data
        focusedField = nil
        // Первая надиктовка без фото: СТОП перед распознаванием — экран разбора
        // (добавить чек / распознать / отменить). Правка черновика или запись
        // поверх уже приложенного фото уходят сразу (см. `stopsAtReview`).
        if AddExpenseViewModel.stopsAtReview(
            isEmptyForm: model.isEmptyForm, hasOtherCapture: capture.imageData != nil
        ) {
            isReviewPresented = true
            return
        }
        sendParse(image: capture.imageData)
    }

    /// Фото чека доставлено (камера/галерея). То же правило, что и для голоса:
    /// первый ввод в пустую форму ждёт решения на экране разбора, снимок для
    /// уточнения черновика или досыл к уже записанной диктовке — сразу в разбор.
    private func handleCaptured(image data: Data) {
        if AddExpenseViewModel.stopsAtReview(
            isEmptyForm: model.isEmptyForm, hasOtherCapture: lastAudio != nil
        ) {
            isReviewPresented = true
            return
        }
        sendParse(image: data)
    }

    /// «Добавить голосом» на экране разбора фото — зеркало «Добавить фото чека»
    /// на экране разбора диктовки. Оверлей закрывает нижнюю панель, удерживать
    /// микрофон негде: пишем в ЗАКРЕПЛЁННОМ режиме («Готово»/«Отмена» на
    /// оверлее записи), как под VoiceOver. «Готово» уходит в разбор сразу
    /// вместе с фото — второй остановки не будет.
    private func startVoiceFromReview() {
        guard !recorder.isRecording, !isRecordingLocked else { return }
        if aiDisabled {
            nudgeAIUnavailable()
            return
        }
        isRecordingLocked = true
        Haptics.tap()
        startRecordingIfNeeded()
    }

    /// После УДАЧНОГО разбора надиктовка больше не нужна: иначе следующий
    /// запрос (например, фото чека) уходил бы вместе со старым голосом поверх
    /// уже применённого черновика — сервер применял его повторно, и позиции
    /// задваивались. При ошибке запись остаётся: алерт предлагает «Повторить».
    private func clearAudioAfterParse() {
        guard model.parseRetryMessage == nil else { return }
        lastAudio = nil
    }

    /// Отправить фото чека на распознавание (вместе с текущим черновиком —
    /// сервер уточнит цены/позиции, не пересобирая распределение). Фото
    /// ОСТАЁТСЯ в форме (`capture`) и прикладывается к последующим голосовым
    /// правкам до закрытия формы.
    private func sendParse(image: Data? = nil) {
        isReviewPresented = false
        model.startParse(api: session.api, audio: lastAudio, image: image) {
            recorder.reset()
            focusedField = nil
            clearAudioAfterParse()
        }
    }

    // MARK: Чек (позиции)

    /// Секция распознанного чека: карточка-чек, подсказки по нераспознанным
    /// именам и вопросам модели, разбивка «С кого сколько» и карточка
    /// переопределения деления.
    private var receiptSection: some View {
        VStack(spacing: 14) {
            ReceiptView(
                items: model.draftItemList,
                members: model.members,
                currency: model.currency,
                onEditItem: { index in itemEditTarget = ItemEditTarget(index: index) },
                onResolveUnknown: { index, name in
                    unknownTarget = UnknownTarget(itemIndex: index, name: name)
                },
                onToggleSurchargeRule: { index in
                    Haptics.tap()
                    withAnimation(.spring(duration: 0.25)) {
                        model.toggleSurchargeRule(at: index)
                    }
                },
                highlightedIndices: model.changedItemIndices
            )
            .task(id: model.changedItemIndices) {
                // Подсветка правки — вспышка: гаснет сама через пару секунд.
                guard !model.changedItemIndices.isEmpty else { return }
                try? await Task.sleep(for: .seconds(2.5))
                // try? глотает CancellationError: без явной проверки следующий
                // разбор, сменивший id задачи, гасил бы СВОЮ ЖЕ свежую подсветку.
                guard !Task.isCancelled else { return }
                withAnimation(.easeOut(duration: 0.6)) {
                    model.clearChangeHighlights()
                }
            }
            if model.hasUnknownItems, let name = model.firstUnknownName {
                Label("Выберите, кто это — «\(name)»: коснитесь красной метки в чеке",
                      systemImage: "exclamationmark.triangle.fill")
                    .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                    .foregroundStyle(Color.negative)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            if model.hasPricelessItems {
                Label("Укажите цену — коснитесь позиции с меткой «цена?». Или продиктуйте: «пицца стоила 600»",
                      systemImage: "exclamationmark.triangle.fill")
                    .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                    .foregroundStyle(Color.negative)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            if !model.parseQuestions.isEmpty {
                ForEach(model.parseQuestions, id: \.self) { question in
                    Label(question, systemImage: "questionmark.circle")
                        .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                        .foregroundStyle(Color.inkSecondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            // Разбивку по людям при неопределённых ценах не показываем:
            // частичные суммы вводят в заблуждение.
            if !model.hasPricelessItems, let shares = model.personShares, !shares.isEmpty {
                PersonBreakdownCard(
                    shares: shares,
                    members: model.members,
                    currency: model.currency,
                    meId: session.me?.id
                )
            }
            // AI мог пропустить блюдо — путь добавить руками, не передиктовывая.
            addItemLink
            splitOverrideCard
        }
    }

    /// «+ Добавить позицию»: пустая строка чека, шит открывается сразу.
    private var addItemLink: some View {
        Button {
            Haptics.tap()
            if let index = model.addBlankItem() {
                itemEditTarget = ItemEditTarget(index: index)
            }
        } label: {
            Label("Добавить позицию", systemImage: "plus.circle.fill")
                .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                .foregroundStyle(Color.accent)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 6)
        }
        .buttonStyle(.plain)
    }

    /// Инлайн-баннер результата голосовой правки (в потоке формы, НЕ поверх
    /// содержимого — плавающая карточка читалась как «ничего не сохранилось»):
    /// статус + ссылка «Отменить». Изменённые строки подсвечены в самом чеке.
    /// Гаснет сам через 6 секунд.
    private var correctionBanner: some View {
        HStack(spacing: 10) {
            Image(systemName: "wand.and.stars")
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(Color.accent)
            VStack(alignment: .leading, spacing: 2) {
                Text("Правка применена")
                    .scaledFont(size: 14, weight: .semibold)
                    .foregroundStyle(Color.ink)
                Text(model.hasDraftItems ? "Изменения подсвечены в чеке" : "Черновик обновлён")
                    .scaledFont(size: 12, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
            }
            Spacer(minLength: 8)
            Button {
                Haptics.tap()
                withAnimation(.spring(duration: 0.3)) { model.undoParse() }
            } label: {
                Text("Отменить")
                    .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                    .foregroundStyle(Color.accent)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 7)
                    .background(Color.accent.opacity(0.12), in: Capsule())
            }
            .buttonStyle(.plain)
        }
        .padding(14)
        .background(Color.accent.opacity(0.1), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .transition(.opacity.combined(with: .move(edge: .top)))
        .task(id: model.changedItemIndices) {
            try? await Task.sleep(for: .seconds(6))
            // Без проверки отмены баннер умирал через 2.5 с, а не через 6: сброс
            // changedItemIndices выше меняет id, задача отменяется, try? глотает
            // CancellationError — и dismissUndo сносил undoSnapshot, делая
            // голосовую правку неоткатываемой.
            guard !Task.isCancelled else { return }
            withAnimation(.spring(duration: 0.3)) { model.dismissUndo() }
        }
    }

    /// Карточка переопределения деления: «Поровну на всех» сбрасывает позиции
    /// (плоское равное деление), «По позициям» — текущее состояние.
    private var splitOverrideCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Сейчас делится по позициям")
                    .sectionHeaderStyle()
                Spacer(minLength: 8)
                Button {
                    Haptics.tap()
                    // Через undo-механизм: сброс чека деструктивен, баннер
                    // «Отменить» возвращает позиции, если нажали случайно.
                    withAnimation(.spring(duration: 0.25)) { model.collapseToEqualSplit() }
                } label: {
                    Text("Поровну на всех")
                }
                .buttonStyle(.softChip(isSelected: false))
            }
            Text("«Поровну» отбросит позиции чека и поделит сумму на всех участников поровну. Вернуть можно кнопкой «Отменить»")
                .scaledFont(size: 12, relativeTo: .footnote)
                .foregroundStyle(Color.inkSecondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
    }

    // MARK: Выбор группы

    private var groupPicker: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("С кем делите расход?")
                .sectionHeaderStyle()
            if model.rooms.isEmpty {
                Text("У вас пока нет групп — создайте её на вкладке «Группы»")
                    .font(.footnote)
                    .foregroundStyle(Color.inkSecondary)
            } else {
                // Автоскролл к выбранной группе: при 6+ группах активный чип
                // мог оказаться за экраном — непонятно, выбрано ли что-то.
                ScrollViewReader { proxy in
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            ForEach(model.rooms) { room in
                                groupChip(room).id(room.id)
                            }
                        }
                    }
                    .onAppear {
                        if let selected = model.selectedRoomId {
                            proxy.scrollTo(selected, anchor: .center)
                        }
                    }
                    .onChange(of: model.selectedRoomId) { _, selected in
                        guard let selected else { return }
                        withAnimation(.spring(duration: 0.3)) {
                            proxy.scrollTo(selected, anchor: .center)
                        }
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
        .modifier(NudgeHighlight(trigger: groupNudge))
    }

    private func groupChip(_ room: RoomSummary) -> some View {
        Button {
            Haptics.tap()
            model.selectRoom(room)
            // Нудж «выберите группу» выполнен — гасим тост сразу, не ждём
            // его таймера: иначе подсказка висит уже над выбранной группой.
            if model.toastMessage == Self.selectGroupToast {
                model.toastMessage = nil
            }
        } label: {
            Text(room.name)
                .lineLimit(1)
        }
        .buttonStyle(.softChip(isSelected: room.id == model.selectedRoomId))
    }

    // MARK: Карточка «что и сколько»

    /// Описание сверху, hairline-разделитель, крупная сумма по центру.
    /// В режиме чека сумма — производная от позиций: показываем её крупно,
    /// но read-only (править — через позиции чека, а не затирая их).
    private func expenseCard(description: Binding<String>, sum: Binding<String>) -> some View {
        VStack(spacing: 16) {
            descriptionField(text: description)
            Rectangle()
                .fill(Color.hairline)
                .frame(height: 1)
            if model.hasDraftItems {
                derivedTotal
            } else {
                sumField(text: sum)
            }
        }
        .surfaceCard()
    }

    /// Крупный итог itemized-черновика (read-only): сумма выводится из позиций,
    /// подпись объясняет, откуда она и где её править.
    private var derivedTotal: some View {
        VStack(spacing: 4) {
            MoneyText(
                model.itemizedTotal ?? (model.itemizedSubtotal + model.itemizedSurcharges),
                role: .neutral,
                size: 40,
                currency: model.currency
            )
            Text(model.hasPricelessItems ? "не все цены указаны" : "по позициям чека")
                .scaledFont(size: 12, relativeTo: .footnote)
                .foregroundStyle(model.hasPricelessItems ? Color.negative : Color.inkSecondary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 4)
    }

    private func descriptionField(text: Binding<String>) -> some View {
        HStack(spacing: 12) {
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(Color.ink.opacity(0.06))
                .frame(width: 44, height: 44)
                .overlay {
                    Image(systemName: "doc.plaintext")
                        .font(.system(size: 20))
                        .foregroundStyle(Color.inkSecondary)
                }
            TextField("Описание", text: text)
                .scaledFont(size: 19, weight: .medium)
                .foregroundStyle(Color.ink)
                .focused($focusedField, equals: .description)
                .submitLabel(.next)
                .onSubmit { focusedField = .sum }
        }
    }

    /// Крупное поле суммы: rounded + monospacedDigit, по центру карточки;
    /// слева — символ валюты выбранной группы.
    private func sumField(text: Binding<String>) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(currencySymbol(model.currency))
                .scaledFont(size: 28, weight: .medium, relativeTo: .title)
                .foregroundStyle(Color.inkSecondary)
            TextField("0", text: text)
                .scaledFont(size: 40, weight: .semibold, relativeTo: .title)
                .monospacedDigit()
                .foregroundStyle(Color.ink)
                .multilineTextAlignment(.center)
                .fixedSize()
                .keyboardType(.numberPad)
                .focused($focusedField, equals: .sum)
                .onChange(of: text.wrappedValue) { _, newValue in
                    // Только целые рубли, без лидирующих нулей-простыней.
                    let filtered = String(newValue.filter(\.isNumber).prefix(9))
                    if filtered != newValue {
                        text.wrappedValue = filtered
                    }
                    // Ручная правка суммы сбрасывает распознанный чек (позиции больше
                    // не источник правды). Только когда пользователь сам в поле —
                    // программное заполнение из `apply(parse:)` фокус на .sum не ставит.
                    if focusedField == .sum, model.hasDraftItems {
                        model.resetItems()
                    }
                }
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
        .contentShape(Rectangle())
        .onTapGesture { focusedField = .sum }
    }

    // MARK: «Заплатил(а) X» — режим чека

    /// Компактная строка плательщика над чеком (см. `showsPayerLine`): способ
    /// деления в этом режиме задают позиции, а плательщик — нет, и выбрать его
    /// больше негде. Тап открывает тот же `PayerPickerView`, что и в карточке
    /// деления.
    private var payerLineCard: some View {
        HStack(spacing: 6) {
            Text(payerIsMe ? "Заплатили" : "Заплатил(а)")
                .foregroundStyle(Color.ink)
            segmentButton(payerLabel) {
                isPayerPickerPresented = true
            }
            Spacer(minLength: 0)
        }
        .scaledFont(size: 15)
        .lineLimit(1)
        .minimumScaleFactor(0.7)
        .disabled(model.members.isEmpty)
        .opacity(model.members.isEmpty ? 0.4 : 1)
        .frame(maxWidth: .infinity)
        .surfaceCard()
    }

    // MARK: «Заплатили вы и разделено поровну / по суммам»

    private var splitCard: some View {
        VStack(spacing: 14) {
            splitModePicker
            splitSentence
            if model.splitType == .equally {
                if !model.members.isEmpty {
                    Text(model.splitHint)
                        .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                        .monospacedDigit()
                        .foregroundStyle(Color.inkSecondary)
                }
            } else {
                amountRows
                distributionStatus
            }
        }
        .frame(maxWidth: .infinity)
        .surfaceCard()
    }

    /// Переключатель способа деления: «Поровну» | «По суммам» (чипы ДС).
    private var splitModePicker: some View {
        HStack(spacing: 8) {
            splitModeChip("Поровну", type: .equally)
            splitModeChip("По суммам", type: .byExactAmount)
        }
    }

    private func splitModeChip(_ title: LocalizedStringKey, type: SplitType) -> some View {
        Button {
            guard model.splitType != type else { return }
            Haptics.tap()
            withAnimation(.spring(duration: 0.25)) {
                model.splitType = type
            }
        } label: {
            Text(title)
        }
        .buttonStyle(.softChip(isSelected: model.splitType == type))
        .disabled(model.members.isEmpty)
        .opacity(model.members.isEmpty ? 0.4 : 1)
    }

    private var splitSentence: some View {
        HStack(spacing: 6) {
            Text(payerIsMe ? "Заплатили" : "Заплатил(а)")
                .foregroundStyle(Color.ink)
            segmentButton(payerLabel) {
                isPayerPickerPresented = true
            }
            Text("и разделено")
                .foregroundStyle(Color.ink)
            segmentButton(model.splitType == .equally
                ? String(localized: "поровну")
                : String(localized: "по суммам")) {
                isSplitPickerPresented = true
            }
        }
        .scaledFont(size: 15)
        .lineLimit(1)
        .minimumScaleFactor(0.7)
        .disabled(model.members.isEmpty)
        .opacity(model.members.isEmpty ? 0.4 : 1)
    }

    // MARK: Режим «По суммам»: поля долей и остаток

    /// Строки выбранных участников с полями точных сумм.
    private var amountRows: some View {
        VStack(spacing: 0) {
            ForEach(model.selectedMembers) { member in
                amountRow(member)
                if member.id != model.selectedMembers.last?.id {
                    Rectangle()
                        .fill(Color.hairline)
                        .frame(height: 1)
                        .padding(.leading, 44)
                }
            }
        }
    }

    private func amountRow(_ member: User) -> some View {
        HStack(spacing: 12) {
            UserAvatarView(user: member, size: 32)
            Text(memberLabel(member.displayName, isMe: member.id == session.me?.id))
                .scaledFont(size: 15)
                .foregroundStyle(Color.ink)
                .lineLimit(1)
            Spacer(minLength: 8)
            TextField("0", text: amountBinding(for: member.id))
                .scaledFont(size: 17, weight: .semibold)
                .monospacedDigit()
                .foregroundStyle(Color.ink)
                .multilineTextAlignment(.trailing)
                .keyboardType(.numberPad)
                .frame(width: 90)
                .focused($focusedField, equals: .amount(member.id))
            Text(currencySymbol(model.currency))
                .scaledFont(size: 15)
                .foregroundStyle(Color.inkSecondary)
        }
        .padding(.vertical, 8)
    }

    /// Биндинг текста доли участника: только цифры, максимум 9 знаков.
    private func amountBinding(for userId: Int) -> Binding<String> {
        Binding(
            get: { model.amountTexts[userId] ?? "" },
            set: { model.amountTexts[userId] = String($0.filter(\.isNumber).prefix(9)) }
        )
    }

    /// Живой остаток: «Осталось распределить: X ₽», перерасход — negative-цветом.
    private var distributionStatus: some View {
        Text(model.distributionHint)
            .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
            .monospacedDigit()
            .foregroundStyle(distributionStatusColor)
            .contentTransition(.numericText())
            .animation(.spring(duration: 0.25), value: model.remainingToDistribute)
    }

    private var distributionStatusColor: Color {
        if model.remainingToDistribute < 0 {
            return .negative
        }
        if model.isDistributionBalanced {
            return .accent
        }
        return .inkSecondary
    }

    private var payerIsMe: Bool {
        model.payerId == session.me?.id
    }

    private var payerLabel: String {
        if payerIsMe {
            return String(localized: "вы")
        }
        return model.payer?.displayName ?? "…"
    }

    private func segmentButton(_ title: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
        }
        .buttonStyle(.softChip)
    }
}

// MARK: - «Кто заплатил?»

private struct PayerPickerView: View {
    let model: AddExpenseViewModel
    let meId: Int?

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List(model.members) { member in
                Button {
                    Haptics.tap()
                    model.payerId = member.id
                    dismiss()
                } label: {
                    HStack(spacing: 12) {
                        UserAvatarView(user: member, size: 36)
                        Text(memberLabel(member.displayName, isMe: member.id == meId))
                            .foregroundStyle(Color.ink)
                        Spacer()
                        if member.id == model.payerId {
                            Image(systemName: "checkmark")
                                .fontWeight(.semibold)
                                .foregroundStyle(Color.accent)
                        }
                    }
                }
                .listRowBackground(Color.surface)
                .listRowSeparatorTint(Color.hairline)
            }
            .scrollContentBackground(.hidden)
            .background(Color.bg)
            .navigationTitle("Кто заплатил?")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Отмена") { dismiss() }
                }
            }
        }
        .tint(Color.accent)
        // Высота шторки: при >4 участников — 4,5 строки, чтобы пятая
        // выглядывала наполовину и было видно, что список скроллится.
        .presentationDetents(
            model.members.count > 4
                ? [.height(64 + 52 * 4.5), .large]
                : [.medium, .large]
        )
    }
}

// MARK: - «Разделить между»

private struct SplitPickerView: View {
    let model: AddExpenseViewModel
    let meId: Int?

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List(model.members) { member in
                Button {
                    Haptics.tap()
                    model.toggleRecipient(member.id)
                } label: {
                    HStack(spacing: 12) {
                        UserAvatarView(user: member, size: 36)
                        Text(memberLabel(member.displayName, isMe: member.id == meId))
                            .foregroundStyle(Color.ink)
                        Spacer()
                        Image(systemName: model.recipientIds.contains(member.id)
                            ? "checkmark.circle.fill"
                            : "circle")
                            .font(.title3)
                            .foregroundStyle(model.recipientIds.contains(member.id)
                                ? Color.accent
                                : Color.inkSecondary.opacity(0.4))
                    }
                }
                .listRowBackground(Color.surface)
                .listRowSeparatorTint(Color.hairline)
            }
            .scrollContentBackground(.hidden)
            .background(Color.bg)
            .navigationTitle("Разделить между")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Готово") { dismiss() }
                        .disabled(model.recipientIds.isEmpty)
                }
            }
            .safeAreaInset(edge: .bottom) {
                // Подсказка по режиму: предпросмотр равных долей или остаток сумм.
                Text(model.splitType == .equally ? model.splitHint : model.distributionHint)
                    .scaledFont(size: 15, weight: .medium)
                    .monospacedDigit()
                    .foregroundStyle(hintColor)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 12)
                    .background(.bar)
            }
        }
        .tint(Color.accent)
        // Высота шторки: при >4 участников — 4,5 строки, чтобы пятая
        // выглядывала наполовину и было видно, что список скроллится.
        .presentationDetents(
            model.members.count > 4
                ? [.height(64 + 52 * 4.5), .large]
                : [.medium, .large]
        )
    }

    private var hintColor: Color {
        if model.recipientIds.isEmpty {
            return .negative
        }
        if model.splitType == .byExactAmount, model.remainingToDistribute < 0 {
            return .negative
        }
        return .inkSecondary
    }
}

/// Встряска + вспышка акцентной рамки для карточки, требующей внимания:
/// подсказывает, КАКОЕ поле заполнить (тап по микрофону без выбранной группы).
/// Радиус рамки = радиус `surfaceCard` (20, continuous).
private struct NudgeHighlight: ViewModifier {
    let trigger: Int
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content
            .phaseAnimator([0, -9, 8, -6, 4, 0], trigger: trigger) { view, phase in
                view
                    .offset(x: reduceMotion ? 0 : phase)
                    .overlay {
                        RoundedRectangle(cornerRadius: 20, style: .continuous)
                            .strokeBorder(Color.accent, lineWidth: 2)
                            .opacity(phase == 0 ? 0 : 1)
                    }
            } animation: { _ in .easeInOut(duration: 0.09) }
    }
}

/// Полноэкранный оверлей записи голоса (hold-to-talk).
/// Сверху — явная зона отмены (✕) с бегущими вверх стрелками; в центре —
/// транскрипт-«телесуфлёр» (новые слова внизу, старые уезжают вверх и тают);
/// внизу — микрофон, который ЕДЕТ ЗА ПАЛЬЦЕМ к зоне отмены (`dragOffset`).
struct RecordingOverlay: View {
    /// Запись активна (палец на кнопке или движок пишет). Оверлей смонтирован
    /// в дереве ПОСТОЯННО и в покое полностью прозрачен: нажатие лишь
    /// проявляет готовые блюр и контент — ноль затрат на монтирование в самый
    /// чувствительный к задержке момент. Дефолт true — для статических
    /// рендеров (снапшот-тесты).
    var isActive: Bool = true
    let transcript: String
    let isCancelling: Bool
    /// Запись закреплена свайпом вверх: палец убран, снизу кнопки «Отмена»/«Готово».
    let isLocked: Bool
    /// Движок записи ещё поднимается: вокруг микрофона крутится дуга
    /// «подключаю», статус говорит правду — «Начинаю запись…».
    var isPreparing: Bool = false
    /// Смещение пальца от точки нажатия (вверх — к замку, влево — к отмене).
    let drag: CGSize
    /// Старт записи — кольцо-прогресс лимита (минута) вокруг микрофона.
    var startedAt: Date? = nil
    /// Живая громкость голоса 0…1: волна и «дыхание» микрофона реагируют
    /// на реальный звук — видно, что запись идёт и тебя слышно.
    var level: CGFloat = 0
    /// Фрейм нажатой кнопки-микрофона (global): оверлей рисует микрофон ровно
    /// там же и того же размера. .zero — дефолт (низ по центру).
    var micFrame: CGRect = .zero
    /// «Что осталось уточнить» (цены, имена, вопросы модели) — подсказка,
    /// что говорить при правке. Пусто — блок не показывается.
    var hints: [String] = []
    /// Кнопки закреплённого режима: «Готово» (стоп → распознавание) и «Отмена».
    var onStop: () -> Void = {}
    var onCancel: () -> Void = {}

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var pulse = false
    /// Вращение дуги «подключаю микрофон».
    @State private var spin = false

    /// Прогресс до замка (вверх) и до отмены (влево): 0…1.
    private var lockProgress: CGFloat { min(1, max(0, -drag.height / 70)) }
    private var cancelProgress: CGFloat { min(1, max(0, -drag.width / 70)) }

    var body: some View {
        GeometryReader { geo in
            let global = geo.frame(in: .global)
            // Центр и размер микрофона = фактическая нажатая кнопка
            // (или дефолт — низ по центру, пока фрейм не измерен).
            let center = micFrame == .zero
                ? CGPoint(x: geo.size.width / 2, y: geo.size.height - 86)
                : CGPoint(x: micFrame.midX - global.minX, y: micFrame.midY - global.minY)
            let micSize = micFrame == .zero ? 82 : micFrame.width
            // Контент выше микрофона и замка над ним.
            let bottomInset = geo.size.height - center.y + micSize / 2 + (isLocked ? 52 : 100)

            ZStack {
                // Фон проявляется ИЗ нажатия быстрым fade (не снапом и не
                // медленно): резкое затемнение читалось как «сначала открылся
                // экран, потом анимация», долгий fade — как задержка.
                Group {
                    Rectangle()
                        .fill(.ultraThinMaterial)
                        .environment(\.colorScheme, .dark)
                        .ignoresSafeArea()
                    Color.black.opacity(0.35).ignoresSafeArea()
                }
                .opacity(isActive ? 1 : 0)
                .animation(.easeOut(duration: 0.12), value: isActive)

                VStack(spacing: 0) {
                    if !hints.isEmpty, !isCancelling {
                        hintsCard
                            .padding(.top, 70)
                    }
                    Spacer(minLength: 12)
                    transcriptWindow
                    Spacer(minLength: 16)
                    // active гейтится по isActive: скрытый (постоянно
                    // смонтированный) оверлей не должен крутить 30 fps.
                    Waveform(active: isActive && !reduceMotion && !isCancelling, level: level)
                        .frame(width: 240, height: 44)
                        .opacity(isCancelling ? 0.25 : 1)
                    if let startedAt {
                        timerLabel(startedAt: startedAt)
                            .padding(.top, 12)
                    }
                    statusTexts
                        .padding(.top, 12)
                }
                .padding(.horizontal, 34)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottom)
                .padding(.bottom, bottomInset)
                .opacity(isActive ? 1 : 0)
                .animation(.easeOut(duration: 0.15), value: isActive)

                Group {
                    if !isLocked {
                        lockZone
                            .position(x: center.x, y: center.y - micSize / 2 - 58)
                    } else {
                        Text("Нажмите, когда закончите")
                            .scaledFont(size: 12, weight: .medium, relativeTo: .footnote)
                            .foregroundStyle(.white.opacity(0.7))
                            .position(x: center.x, y: center.y - micSize / 2 - 24)
                    }
                    cancelControl
                        // В отмене главный крест — сам микрофон (кроссфейд в
                        // красный); малую ✕-зону гасим — не двоить кресты.
                        .opacity(isCancelling ? 0.25 : 1)
                        .position(x: max(58, center.x - micSize / 2 - 66), y: center.y)
                }
                .opacity(isActive ? 1 : 0)
                .animation(.easeOut(duration: 0.15), value: isActive)

                micOrStop(size: micSize)
                    .position(center)
            }
        }
        .animation(.spring(duration: 0.2), value: isCancelling)
        .animation(.spring(duration: 0.25), value: isLocked)
        // Лейаут оверлея завязан на геометрию кнопки — гигантские настройки
        // текста ломают его; ограничиваем масштаб разумным максимумом.
        .dynamicTypeSize(...DynamicTypeSize.accessibility1)
        // repeatForever-анимации перезапускаются на каждое нажатие: оверлей
        // смонтирован постоянно, и старая анимация (с onAppear формы) не
        // подхватилась бы вью, вставленными в дерево позже.
        .onChange(of: isActive) { _, on in
            guard on, !reduceMotion else { return }
            spin = false
            withAnimation(.linear(duration: 0.9).repeatForever(autoreverses: false)) { spin = true }
        }
        // Пульс-кольцо входит в дерево, когда движок реально записал
        // (isPreparing → false) — анимацию заводим в этот же момент.
        .onChange(of: isPreparing) { _, preparing in
            guard !preparing, isActive, !reduceMotion else { return }
            pulse = false
            withAnimation(.easeOut(duration: 1.4).repeatForever(autoreverses: false)) { pulse = true }
        }
    }

    // MARK: Управление

    /// Пока палец на кнопке: замок над микрофоном (свайп вверх), ✕ слева
    /// (свайп влево), микрофон едет за пальцем. Закреплено: кнопки «Отмена»/«Готово».
    /// ✕ слева от микрофона: до замка — индикатор свайпа влево,
    /// после замка — обычная кнопка отмены.
    @ViewBuilder
    private var cancelControl: some View {
        if isLocked {
            Button(action: onCancel) {
                cancelZone.contentShape(Circle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Отменить запись")
        } else {
            cancelZone
        }
    }

    /// Микрофон (запись) или кнопка «стоп» (закреплено) — на месте нажатой
    /// кнопки, того же размера.
    @ViewBuilder
    private func micOrStop(size: CGFloat) -> some View {
        if isLocked {
            lockedStopButton(size: size)
                .transition(.scale(scale: 0.85).combined(with: .opacity))
        } else {
            micCircle(size: size)
                .transition(.scale(scale: 0.85).combined(with: .opacity))
        }
    }

    private func lockedStopButton(size: CGFloat) -> some View {
        Button(action: onStop) {
            ZStack {
                ring(size: size)
                Circle()
                    .fill(LinearGradient(colors: [Color.accent, Color.accentPressed],
                                         startPoint: .topLeading, endPoint: .bottomTrailing))
                    .frame(width: size, height: size)
                    .overlay {
                        Image(systemName: "stop.fill")
                            .font(.system(size: size * 0.33, weight: .semibold))
                            .foregroundStyle(.white)
                    }
                    .shadow(color: Color.accent.opacity(0.5), radius: 20, y: 8)
            }
            .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Завершить запись и распознать")
    }

    /// Кольцо-прогресс лимита записи (минута) вокруг круга диаметром `size`.
    @ViewBuilder
    private func ring(size: CGFloat) -> some View {
        if let startedAt {
            TimelineView(.animation(minimumInterval: 0.2)) { timeline in
                let progress = min(1, timeline.date.timeIntervalSince(startedAt) / 60)
                Circle()
                    .trim(from: 0, to: progress)
                    .stroke(
                        progress > 0.85 ? Color.negative : Color.white.opacity(0.9),
                        style: StrokeStyle(lineWidth: 3.5, lineCap: .round)
                    )
                    .rotationEffect(.degrees(-90))
                    .frame(width: size + 16, height: size + 16)
            }
        }
    }

    /// Замок над микрофоном: свайп вверх — запись без удержания (как в Telegram).
    private var lockZone: some View {
        VStack(spacing: 5) {
            ZStack {
                Circle().fill(lockProgress > 0.99 ? Color.accent : Color.white.opacity(0.12))
                Circle().strokeBorder(Color.white.opacity(0.3), lineWidth: 1.5)
                Image(systemName: lockProgress > 0.5 ? "lock.fill" : "lock.open")
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(.white.opacity(0.9))
            }
            .frame(width: 44, height: 44)
            .scaleEffect(1 + lockProgress * 0.3)
            Image(systemName: "chevron.up")
                .font(.system(size: 11, weight: .bold))
                .foregroundStyle(.white.opacity(0.55))
        }
        .animation(.spring(duration: 0.15), value: lockProgress)
    }

    /// Зона отмены слева: свайп влево — выбросить запись.
    private var cancelZone: some View {
        HStack(spacing: 5) {
            Image(systemName: "chevron.left")
                .font(.system(size: 11, weight: .bold))
                .foregroundStyle(.white.opacity(0.55))
            ZStack {
                Circle().fill(isCancelling ? Color.negative : Color.white.opacity(0.12))
                Circle().strokeBorder(
                    isCancelling ? Color.negative : Color.white.opacity(0.3), lineWidth: 1.5)
                Image(systemName: "xmark")
                    .font(.system(size: 15, weight: .bold))
                    .foregroundStyle(.white.opacity(0.9))
            }
            .frame(width: 44, height: 44)
            .scaleEffect(1 + cancelProgress * 0.3)
        }
        .animation(.spring(duration: 0.15), value: cancelProgress)
    }

    /// Таймер записи: красная мигающая точка, прошедшее время и «осталось N с»
    /// на последних секундах лимита (минута).
    private func timerLabel(startedAt: Date) -> some View {
        TimelineView(.periodic(from: startedAt, by: 0.5)) { timeline in
            let elapsed = min(60, timeline.date.timeIntervalSince(startedAt))
            let remaining = max(0, 60 - Int(elapsed))
            let blinkOn = Int(elapsed * 2) % 2 == 0
            HStack(spacing: 8) {
                Circle()
                    .fill(Color.negative)
                    .frame(width: 8, height: 8)
                    .opacity(reduceMotion ? 1 : (blinkOn ? 1 : 0.25))
                Text(String(format: "%d:%02d", Int(elapsed) / 60, Int(elapsed) % 60))
                    .scaledFont(size: 18, weight: .bold)
                    .monospacedDigit()
                    .foregroundStyle(.white)
                if remaining <= 15 {
                    Text("· осталось \(remaining) с")
                        .scaledFont(size: 14, weight: .semibold)
                        .monospacedDigit()
                        .foregroundStyle(Color.negative)
                }
            }
        }
    }

    private var statusTexts: some View {
        let title: String
        let subtitle: String
        if isCancelling {
            title = String(localized: "Отпустите — отмена")
            subtitle = String(localized: "Верните палец, чтобы продолжить")
        } else if isLocked {
            title = String(localized: "Запись идёт")
            subtitle = String(localized: "Говорите свободно — палец держать не нужно")
        } else if isPreparing {
            title = String(localized: "Начинаю запись…")
            subtitle = String(localized: "Держите палец — микрофон включается")
        } else {
            title = String(localized: "Говорите…")
            subtitle = String(localized: "Отпустите — распознать · вверх — закрепить · влево — отмена")
        }
        return VStack(spacing: 6) {
            Text(title)
                .scaledFont(size: 20, weight: .bold)
                .foregroundStyle(isCancelling ? Color.negative : .white)
            Text(subtitle)
                .scaledFont(size: 13, relativeTo: .footnote)
                .foregroundStyle(.white.opacity(0.75))
                .multilineTextAlignment(.center)
        }
        // Crossfade при смене состояния вместо мгновенной подмены текста.
        .id(title)
        .transition(.opacity)
        .animation(.easeOut(duration: 0.18), value: title)
    }

    /// «Осталось уточнить»: чего не хватает черновику — прямо на экране
    /// диктовки, чтобы было видно, что сказать.
    private var hintsCard: some View {
        VStack(alignment: .leading, spacing: 7) {
            Text("Осталось уточнить")
                .scaledFont(size: 11, weight: .bold, relativeTo: .footnote)
                .tracking(1.2)
                .textCase(.uppercase)
                .foregroundStyle(Color.accent)
            ForEach(hints, id: \.self) { hint in
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Circle().fill(Color.accent).frame(width: 5, height: 5)
                        .padding(.top, 5)
                    Text(hint)
                        .scaledFont(size: 14, weight: .medium)
                        .foregroundStyle(.white.opacity(0.92))
                        .multilineTextAlignment(.leading)
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 13)
        .frame(maxWidth: 320, alignment: .leading)
        .background(Color.white.opacity(0.1), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(Color.white.opacity(0.14), lineWidth: 1)
        )
        .transition(.opacity)
    }

    /// Транскрипт-«телесуфлёр»: окно ~3 строки, выравнивание по низу — новые
    /// слова всегда видны, старые строки уезжают вверх и растворяются в маске.
    private var transcriptWindow: some View {
        Group {
            if transcript.isEmpty {
                Text("Слушаю…")
                    .scaledFont(size: 19, weight: .semibold)
                    .foregroundStyle(.white.opacity(0.45))
            } else {
                Text(transcript)
                    .scaledFont(size: 21, weight: .semibold)
                    .foregroundStyle(.white)
                    .multilineTextAlignment(.center)
                    .lineSpacing(4)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: 320)
        .frame(maxWidth: .infinity)
        .frame(height: 128, alignment: .bottom)
        .clipped()
        .mask(
            LinearGradient(
                stops: [
                    .init(color: .clear, location: 0),
                    .init(color: .black, location: 0.4),
                    .init(color: .black, location: 1),
                ],
                startPoint: .top, endPoint: .bottom
            )
        )
        .animation(.easeOut(duration: 0.18), value: transcript)
    }

    /// Микрофон размера нажатой кнопки: pop при появлении (видно «нажалось»),
    /// дуга «подключаю» пока движок поднимается, кроссфейд в красный при отмене,
    /// «дыхание» от громкости; вокруг — пульс и кольцо-прогресс лимита.
    private func micCircle(size: CGFloat) -> some View {
        ZStack {
            if !reduceMotion, !isCancelling, !isPreparing {
                Circle()
                    .stroke(Color.accent.opacity(0.5), lineWidth: 2)
                    .frame(width: size, height: size)
                    .scaleEffect(pulse ? 1.7 : 1)
                    .opacity(pulse ? 0 : 0.7)
            }
            ring(size: size)
            // Дуга «подключаю микрофон» — крутится, пока движок поднимается.
            // isActive — чтобы скрытый оверлей не держал вечную анимацию.
            if isPreparing, isActive, !reduceMotion {
                Circle()
                    .trim(from: 0, to: 0.28)
                    .stroke(Color.white.opacity(0.85),
                            style: StrokeStyle(lineWidth: 3, lineCap: .round))
                    .frame(width: size + 16, height: size + 16)
                    .rotationEffect(.degrees(spin ? 360 : 0))
                    .transition(.opacity)
            }
            ZStack {
                // Два слоя градиента с кроссфейдом — плавный уход в красный.
                Circle()
                    .fill(LinearGradient(colors: [Color.accent, Color.accentPressed],
                                         startPoint: .topLeading, endPoint: .bottomTrailing))
                Circle()
                    .fill(LinearGradient(colors: [Color.negative, Color.negative.opacity(0.8)],
                                         startPoint: .topLeading, endPoint: .bottomTrailing))
                    .opacity(isCancelling ? 1 : 0)
                ZStack {
                    Image(systemName: "mic.fill")
                        .opacity(isCancelling ? 0 : 1)
                        .scaleEffect(isCancelling ? 0.6 : 1)
                    Image(systemName: "xmark")
                        .opacity(isCancelling ? 1 : 0)
                        .scaleEffect(isCancelling ? 1 : 0.6)
                }
                .font(.system(size: size * 0.37, weight: .semibold))
                .foregroundStyle(.white)
            }
            .frame(width: size, height: size)
            .opacity(isPreparing ? 0.75 : 1)
            .shadow(color: (isCancelling ? Color.negative : Color.accent).opacity(0.5),
                    radius: 20, y: 8)
            .animation(.spring(duration: 0.22), value: isCancelling)
        }
        // Pop «нажалось»: в покое кружок сжат (как кнопка под пальцем),
        // нажатие пружинит его к полному размеру одновременно с fade фона —
        // раскрытие читается одним движением из нажатия.
        .opacity(isActive ? 1 : 0)
        .animation(.easeOut(duration: 0.1), value: isActive)
        .scaleEffect(reduceMotion || isActive ? 1 : 0.86)
        .animation(.spring(duration: 0.28, bounce: 0.4), value: isActive)
        .scaleEffect(1 + level * 0.1)
        .animation(.easeOut(duration: 0.1), value: level)
        .animation(.spring(duration: 0.25), value: isPreparing)
        .offset(x: max(drag.width, -130) * 0.4, y: max(drag.height, -130) * 0.4)
        .animation(.spring(duration: 0.15), value: drag)
    }
}

/// Анимированная звуковая волна из вертикальных полосок. Амплитуда управляется
/// РЕАЛЬНОЙ громкостью голоса (`level`): молчишь — полоски почти плоские,
/// говоришь — прыгают. Это главный сигнал «запись идёт и тебя слышно».
private struct Waveform: View {
    let active: Bool
    var level: CGFloat = 1
    private let bars = 26

    var body: some View {
        TimelineView(.animation(minimumInterval: active ? 1.0 / 30 : nil, paused: !active)) { timeline in
            let t = timeline.date.timeIntervalSinceReferenceDate
            HStack(spacing: 4) {
                ForEach(0..<bars, id: \.self) { i in
                    let h = barHeight(i: i, t: t)
                    Capsule()
                        .fill(Color.white.opacity(0.9))
                        .frame(width: 4, height: h)
                }
            }
            .frame(maxHeight: .infinity)
            .animation(.easeOut(duration: 0.12), value: level)
        }
    }

    private func barHeight(i: Int, t: TimeInterval) -> CGFloat {
        guard active else { return 8 }
        let phase = Double(i) * 0.55
        let wave = abs(sin(t * 6 + phase))
        let env = abs(sin(Double(i) * 1.9))
        // 0.18 — «тишина» слегка шевелится; голос раскачивает до полной высоты.
        let amp = 0.18 + Double(level) * 1.1
        return 6 + CGFloat(wave * (14 + env * 24) * amp)
    }
}

// MARK: - Шит правки позиции

/// Шит правки одной позиции чека: название и цена; переключатель «Долями /
/// Суммами» (ОДИН контрол на строку — степпер веса ИЛИ поле суммы); участие по
/// тапу на имя; пустое поле суммы = «авто» (доля по весу). У каждого участника —
/// живая рассчитанная сумма, в подвале — остаток/перерасход (как в ручном
/// «По суммам»). У надбавки правится только название/цена (делится по базе).
struct ItemSheetView: View {
    let model: AddExpenseViewModel
    let index: Int
    let meId: Int?
    @Environment(\.dismiss) private var dismiss

    @State private var name: String
    @State private var priceText: String
    /// false — «Долями» (веса-степперы), true — «Суммами» (поля сумм).
    @State private var byAmount: Bool
    @State private var participating: Set<Int>
    @State private var weights: [Int: Int]
    @State private var amounts: [Int: String]
    /// Подтверждение удаления позиции/сбора из чека.
    @State private var isDeleteConfirmPresented = false
    private let isSurcharge: Bool
    private let originalItem: OperationItem?
    /// Клиентский id правимой строки: пока шит открыт, голосовая правка может
    /// переставить/заменить позиции — по индексу «Готово»/«Удалить» попадали
    /// бы в ЧУЖУЮ строку (replaceItem проверяет только границы массива).
    private let itemId: UUID?

    init(model: AddExpenseViewModel, index: Int, meId: Int?) {
        self.model = model
        self.index = index
        self.meId = meId
        let item = model.draftItemList.indices.contains(index) ? model.draftItemList[index] : nil
        self.originalItem = item
        self.itemId = item?.id
        self.isSurcharge = item?.isSurcharge ?? false
        _name = State(initialValue: item?.name ?? "")
        _priceText = State(initialValue: item.map { String($0.price) } ?? "")
        let shares = item?.shareList ?? []
        _participating = State(initialValue: Set(shares.map(\.userId)))
        _byAmount = State(initialValue: shares.contains { $0.amount != nil })
        var w: [Int: Int] = [:]
        var a: [Int: String] = [:]
        for share in shares {
            w[share.userId] = share.weight
            if let amount = share.amount {
                a[share.userId] = String(amount)
            }
        }
        _weights = State(initialValue: w)
        _amounts = State(initialValue: a)
    }

    /// Секции шита (и для body, и для ImageRenderer-снапшотов:
    /// NavigationStack в ImageRenderer не рендерится).
    var sheetSections: some View {
        VStack(spacing: 16) {
            fieldsCard
            if !isSurcharge {
                modePicker
                modeHint
                participantsCard
                splitStatusLine
            }
            // AI мог придумать лишнюю строку — путь удалить её руками,
            // не передиктовывая весь чек.
            Button(role: .destructive) {
                isDeleteConfirmPresented = true
            } label: {
                Label(isSurcharge ? "Удалить сбор" : "Удалить позицию", systemImage: "trash")
                    .scaledFont(size: 15, weight: .medium, relativeTo: .subheadline)
                    .foregroundStyle(Color.negative)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 6)
            }
            .buttonStyle(.plain)
        }
        .padding(20)
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                sheetSections
            }
            .background(Color.bg)
            .navigationTitle(isSurcharge ? "Сбор" : "Позиция")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Отмена") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Готово") {
                        commit()
                        dismiss()
                    }
                    .fontWeight(.semibold)
                    // Несходящееся деление не пишем в чек: canSave всё равно
                    // заблокирует, но пользователь должен видеть причину здесь.
                    .disabled(!isCommittable)
                }
            }
            .confirmationDialog(
                isSurcharge ? "Удалить сбор?" : "Удалить позицию?",
                isPresented: $isDeleteConfirmPresented,
                titleVisibility: .visible
            ) {
                Button("Удалить", role: .destructive) {
                    Haptics.tap()
                    if let itemId {
                        model.deleteItem(id: itemId)
                    }
                    dismiss()
                }
                Button("Отмена", role: .cancel) {}
            } message: {
                Text("Строка исчезнет из чека, итог пересчитается")
            }
        }
        .tint(Color.accent)
    }

    // MARK: Живой расчёт деления

    /// Итог деления позиции по текущему состоянию шита.
    private enum SplitStatus: Equatable {
        /// Деление сходится: userId → сумма (зеркало серверного расчёта).
        case ok([Int: Int])
        case noPrice
        case noParticipants
        /// Все фиксы, до цены не хватает N (некому отдать остаток).
        case under(Int)
        /// Фиксы превышают цену на N.
        case over(Int)
    }

    /// Введённая цена позиции (0 — пусто/невалидно).
    private var price: Int { Int(priceText) ?? 0 }

    /// Фикс участника из поля «Суммами»; nil — «авто» (пустое/нулевое поле).
    private func fixedAmount(_ id: Int) -> Int? {
        guard byAmount, let v = Int(amounts[id] ?? ""), v > 0 else { return nil }
        return v
    }

    /// Доли из текущего состояния шита — ровно та же сборка, что в commit().
    private var currentShares: [ItemShare] {
        model.members
            .filter { participating.contains($0.id) }
            .map { member in
                let id = member.id
                if let amount = fixedAmount(id) {
                    return ItemShare(userId: id, weight: 1, amount: amount)
                }
                return ItemShare(userId: id, weight: byAmount ? 1 : max(1, weights[id] ?? 1))
            }
    }

    private var splitStatus: SplitStatus {
        guard price >= 1 else { return .noPrice }
        guard !participating.isEmpty else { return .noParticipants }
        let fixed = participating.reduce(0) { $0 + (fixedAmount($1) ?? 0) }
        if fixed > price { return .over(fixed - price) }
        let hasAuto = participating.contains { fixedAmount($0) == nil }
        if !hasAuto, fixed < price { return .under(price - fixed) }
        let item = OperationItem(name: "·", price: price, shares: currentShares)
        guard let shares = [item].derivedShares()?.shares else { return .under(price - fixed) }
        return .ok(shares)
    }

    /// Сумма участника при текущем делении (для живой подписи в строке).
    private func liveAmount(_ id: Int) -> Int? {
        if case .ok(let shares) = splitStatus { return shares[id] }
        return nil
    }

    private var isCommittable: Bool {
        if isSurcharge { return price >= 1 }
        if case .ok = splitStatus { return true }
        return false
    }

    /// Подпись остатка/перерасхода под участниками — те же формулировки,
    /// что в ручном режиме «По суммам» (distributionHint).
    private var splitStatusLine: some View {
        Group {
            switch splitStatus {
            case .ok:
                Label("Сумма распределена полностью", systemImage: "checkmark")
                    .foregroundStyle(Color.accent)
            case .noPrice:
                Text("Укажите цену позиции")
                    .foregroundStyle(Color.inkSecondary)
            case .noParticipants:
                Text("Выберите хотя бы одного участника")
                    .foregroundStyle(Color.negative)
            case .under(let rest):
                Text("Осталось распределить: \(money(rest, currency: model.currency))")
                    .foregroundStyle(Color.negative)
            case .over(let extra):
                Text("Перерасход: \(money(extra, currency: model.currency))")
                    .foregroundStyle(Color.negative)
            }
        }
        .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
        .monospacedDigit()
        .frame(maxWidth: .infinity)
        .animation(.spring(duration: 0.25), value: splitStatus)
    }

    /// Подсказка режима — как в прототипе: объясняет, что значат доли/суммы.
    private var modeHint: some View {
        Text(byAmount
             ? "Впишите точную сумму за человека. Пустое поле — «авто»: остаток делится поровну"
             : "Поровну — у всех по одной доле. Съел больше — добавьте долей")
            .scaledFont(size: 12, relativeTo: .footnote)
            .foregroundStyle(Color.inkSecondary)
            .multilineTextAlignment(.center)
            .frame(maxWidth: .infinity)
    }

    private var fieldsCard: some View {
        VStack(spacing: 12) {
            TextField("Название", text: $name)
                .scaledFont(size: 17, weight: .medium)
                .foregroundStyle(Color.ink)
            Rectangle().fill(Color.hairline).frame(height: 1)
            HStack {
                Text("Цена")
                    .scaledFont(size: 15)
                    .foregroundStyle(Color.inkSecondary)
                Spacer()
                TextField("0", text: priceBinding)
                    .scaledFont(size: 17, weight: .semibold)
                    .monospacedDigit()
                    .multilineTextAlignment(.trailing)
                    .keyboardType(.numberPad)
                    .frame(width: 100)
                Text(currencySymbol(model.currency))
                    .scaledFont(size: 15)
                    .foregroundStyle(Color.inkSecondary)
            }
        }
        .surfaceCard()
    }

    private var modePicker: some View {
        Picker("", selection: $byAmount) {
            Text("Долями").tag(false)
            Text("Суммами").tag(true)
        }
        .pickerStyle(.segmented)
    }

    private var participantsCard: some View {
        VStack(spacing: 0) {
            ForEach(model.members) { member in
                participantRow(member)
                if member.id != model.members.last?.id {
                    Rectangle()
                        .fill(Color.hairline)
                        .frame(height: 1)
                        .padding(.leading, 44)
                }
            }
        }
        .surfaceCard(padding: 0)
    }

    private func participantRow(_ member: User) -> some View {
        let isOn = participating.contains(member.id)
        return HStack(spacing: 12) {
            UserAvatarView(user: member, size: 32)
                .opacity(isOn ? 1 : 0.4)
            Button {
                toggle(member.id)
            } label: {
                VStack(alignment: .leading, spacing: 2) {
                    Text(memberLabel(member.displayName, isMe: member.id == meId))
                        .scaledFont(size: 15)
                        .foregroundStyle(isOn ? Color.ink : Color.inkSecondary)
                        .lineLimit(1)
                    if let caption = rowCaption(member.id) {
                        Text(caption)
                            .scaledFont(size: 12, weight: .medium, relativeTo: .footnote)
                            .monospacedDigit()
                            .foregroundStyle(Color.inkSecondary)
                            .contentTransition(.numericText())
                            .animation(.spring(duration: 0.25), value: caption)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if isOn {
                control(member.id)
            } else {
                Image(systemName: "circle")
                    .foregroundStyle(Color.inkSecondary.opacity(0.4))
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
    }

    /// Живая подпись под именем: «×3 · 150 ₽» («Долями») или «авто · 1 250 ₽»
    /// у не-зафиксированных («Суммами»). nil — не участвует или фикс введён
    /// (его сумма и так в поле).
    private func rowCaption(_ userId: Int) -> String? {
        guard participating.contains(userId) else { return nil }
        if byAmount {
            guard fixedAmount(userId) == nil else { return nil }
            guard let amount = liveAmount(userId) else { return String(localized: "авто") }
            return String(localized: "авто · \(money(amount, currency: model.currency))")
        }
        let weight = max(1, weights[userId] ?? 1)
        guard let amount = liveAmount(userId) else { return "×\(weight)" }
        return "×\(weight) · \(money(amount, currency: model.currency))"
    }

    /// Ровно ОДИН контрол на строку: степпер веса («Долями») ИЛИ поле суммы
    /// («Суммами», пустое = «авто»). Рассчитанная сумма — подписью под именем.
    @ViewBuilder
    private func control(_ userId: Int) -> some View {
        if byAmount {
            HStack(spacing: 4) {
                TextField("авто", text: amountBinding(userId))
                    .scaledFont(size: 15, weight: .semibold)
                    .monospacedDigit()
                    .multilineTextAlignment(.trailing)
                    .keyboardType(.numberPad)
                    .frame(width: 72)
                Text(currencySymbol(model.currency))
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(Color.inkSecondary)
            }
        } else {
            Stepper("", value: weightBinding(userId), in: 1...20)
                .labelsHidden()
                .fixedSize()
        }
    }

    private var priceBinding: Binding<String> {
        Binding(
            get: { priceText },
            set: { priceText = String($0.filter(\.isNumber).prefix(9)) }
        )
    }

    private func amountBinding(_ id: Int) -> Binding<String> {
        Binding(
            get: { amounts[id] ?? "" },
            set: { amounts[id] = String($0.filter(\.isNumber).prefix(9)) }
        )
    }

    private func weightBinding(_ id: Int) -> Binding<Int> {
        Binding(
            get: { weights[id] ?? 1 },
            set: { weights[id] = max(1, $0) }
        )
    }

    private func toggle(_ id: Int) {
        if participating.contains(id) {
            participating.remove(id)
        } else {
            participating.insert(id)
            if weights[id] == nil { weights[id] = 1 }
        }
        Haptics.tap()
    }

    /// Пересобирает позицию из состояния шита и пишет обратно в черновик.
    private func commit() {
        guard let original = originalItem, let itemId else { return }
        let price = Int(priceText) ?? original.price
        let trimmedName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        var newShares: [ItemShare]? = nil
        if !isSurcharge {
            newShares = model.members
                .filter { participating.contains($0.id) }
                .map { member -> ItemShare in
                    let id = member.id
                    if byAmount, let amount = Int(amounts[id] ?? ""), amount > 0 {
                        return ItemShare(userId: id, weight: 1, amount: amount)
                    }
                    // Пустое поле = «авто» (по весу); в режиме долей — заданный вес.
                    return ItemShare(userId: id, weight: byAmount ? 1 : max(1, weights[id] ?? 1))
                }
        }
        model.replaceItem(id: itemId, with: OperationItem(
            name: trimmedName.isEmpty ? original.name : trimmedName,
            price: price,
            qty: original.qty,
            shares: newShares,
            kind: original.kind,
            split: original.split,
            percent: original.percent,
            unknown: original.unknown
        ))
    }
}

// MARK: - Выбор участника для нераспознанного имени

/// Пикер участника для нераспознанного имени: выбор пишет алиас на сервер и
/// применяет доли локально (`model.resolveUnknown`).
private struct UnknownPickerView: View {
    let model: AddExpenseViewModel
    let itemIndex: Int
    let name: String
    let api: APIClient
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List(model.members) { member in
                Button {
                    Haptics.tap()
                    model.resolveUnknown(itemIndex: itemIndex, name: name, to: member.id, api: api)
                    dismiss()
                } label: {
                    HStack(spacing: 12) {
                        UserAvatarView(user: member, size: 36)
                        Text(member.displayName)
                            .foregroundStyle(Color.ink)
                        Spacer()
                    }
                }
                .listRowBackground(Color.surface)
                .listRowSeparatorTint(Color.hairline)
            }
            .scrollContentBackground(.hidden)
            .background(Color.bg)
            .navigationTitle("Кто это — «\(name)»?")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Отмена") { dismiss() }
                }
            }
        }
        .tint(Color.accent)
    }
}

#Preview {
    AddExpenseView()
        .environment(SessionStore())
}
