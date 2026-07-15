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
                    // Автофокус: без него форма открывается без курсора
                    // и клавиатуры — неочевидно, что можно печатать сразу.
                    if focusedField == nil {
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
                        await model.parse(api: session.api, image: data)
                        capture.reset()
                    }
                }
                .fullScreenCover(isPresented: $isCameraPresented) {
                    CameraPicker { image in
                        guard capture.setImage(image), let data = capture.imageData else { return }
                        Task {
                            await model.parse(api: session.api, image: data)
                            capture.reset()
                        }
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
                    Button("Ок", role: .cancel) {}
                } message: {
                    Text(model.alertMessage ?? "")
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
            form
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
                if model.isEmptyForm {
                    composerCard
                }
                expenseCard(description: $model.descriptionText, sum: $model.sumText)
                if model.hasDraftItems {
                    receiptSection
                } else {
                    splitCard
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
        .overlay {
            if model.isParsing {
                parsingOverlay
            }
        }
        .safeAreaInset(edge: .bottom) {
            bottomBar
        }
    }

    /// Полупрозрачный оверлей со спиннером на время AI-распознавания.
    private var parsingOverlay: some View {
        ZStack {
            Color.black.opacity(0.12).ignoresSafeArea()
            VStack(spacing: 12) {
                ProgressView()
                Text("Распознаю…")
                    .font(.system(size: 14, weight: .medium, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
            }
            .padding(24)
            .surfaceCard()
        }
    }

    /// Плашка «Нет соединения…»: синхронизированную операцию офлайн
    /// не редактируем (кнопка «Сохранить» заблокирована).
    private var offlineBlockedNotice: some View {
        HStack(spacing: 10) {
            Image(systemName: "wifi.slash")
                .font(.system(size: 16))
                .foregroundStyle(Color.inkSecondary)
            Text("Нет соединения. Можно редактировать только неотправленные операции")
                .font(.system(size: 13, weight: .medium, design: .rounded))
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

    /// Нижняя панель: CTA «Сохранить» + (когда черновик уже есть) компактный
    /// микрофон для голосовой правки под большим пальцем.
    private var bottomBar: some View {
        HStack(spacing: 12) {
            if !model.isEmptyForm {
                micButton
            }
            saveButton
        }
        .padding(.horizontal, 20)
        .padding(.vertical, 8)
        .background(Color.bg)
    }

    /// CTA «Сохранить»: premium pill вместо тулбарной кнопки.
    private var saveButton: some View {
        Button {
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
        // В режиме «По суммам» сохранение доступно только при Σ долей == сумме;
        // itemized-черновик с нераспознанными именами не сохраняется;
        // офлайн-правка синхронизированной операции заблокирована.
        .disabled(model.isSaving || !model.canSave || isOfflineBlocked)
    }

    /// Компактный микрофон в нижней панели (hold-to-talk) — голосовая правка
    /// черновика. Недоступен офлайн (AI требует сети) и во время распознавания.
    private var micButton: some View {
        Circle()
            .fill(recorder.isRecording ? Color.negative : Color.accent)
            .frame(width: 54, height: 54)
            .overlay {
                Image(systemName: recorder.isRecording ? "waveform" : "mic.fill")
                    .font(.system(size: 20, weight: .semibold))
                    .foregroundStyle(.white)
            }
            .opacity(aiDisabled ? 0.45 : 1)
            .scaleEffect(recorder.isRecording ? 1.08 : 1)
            .animation(.spring(duration: 0.2), value: recorder.isRecording)
            .gesture(micHoldGesture)
            .accessibilityLabel("Записать голосом")
    }

    // MARK: AI-композер

    /// Крупный композер на пустой форме: hold-to-talk микрофон + «Сфотографировать
    /// чек». Обе кнопки требуют сети — офлайн заблокированы (прецедент — офлайн-
    /// алерт погашения в SettleUpView).
    private var composerCard: some View {
        VStack(spacing: 16) {
            Text("Опишите расход голосом или сфотографируйте чек")
                .font(.system(size: 14, weight: .medium, design: .rounded))
                .foregroundStyle(Color.inkSecondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)

            Circle()
                .fill(recorder.isRecording ? Color.negative : Color.accent)
                .frame(width: 96, height: 96)
                .overlay {
                    Image(systemName: recorder.isRecording ? "waveform" : "mic.fill")
                        .font(.system(size: 38, weight: .semibold))
                        .foregroundStyle(.white)
                }
                .opacity(aiDisabled ? 0.45 : 1)
                .scaleEffect(recorder.isRecording ? 1.06 : 1)
                .animation(.spring(duration: 0.2), value: recorder.isRecording)
                .gesture(micHoldGesture)
                .accessibilityLabel("Записать голосом")

            Text(recorder.isRecording ? "Отпустите, чтобы распознать" : "Удерживайте, чтобы говорить")
                .font(.system(size: 13, weight: .medium, design: .rounded))
                .foregroundStyle(Color.inkSecondary)

            Button {
                isPhotoSourceDialogPresented = true
            } label: {
                Label("Сфотографировать чек", systemImage: "camera.fill")
                    .font(.system(size: 15, weight: .semibold, design: .rounded))
                    .foregroundStyle(Color.accent)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 14)
                    .background(Color.accent.opacity(0.12), in: Capsule())
                    .contentShape(Capsule())
            }
            .buttonStyle(.plain)
            .disabled(aiDisabled)
            .opacity(aiDisabled ? 0.45 : 1)

            if aiDisabled, !session.isOnline {
                Text("Распознавание доступно только онлайн")
                    .font(.system(size: 12, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
            }
        }
        .frame(maxWidth: .infinity)
        .surfaceCard()
    }

    /// AI-действия недоступны: нет сети (распознавание — только онлайн) или уже
    /// идёт запрос.
    private var aiDisabled: Bool {
        !session.isOnline || model.isParsing
    }

    /// Hold-to-talk: нажали — начинаем запись, отпустили — стоп и распознавание.
    private var micHoldGesture: some Gesture {
        DragGesture(minimumDistance: 0)
            .onChanged { _ in
                guard !aiDisabled else { return }
                startRecordingIfNeeded()
            }
            .onEnded { _ in
                stopRecordingAndParse()
            }
    }

    /// Старт записи по нажатию (idempotent): запрашивает доступ к микрофону при
    /// первом использовании, затем поднимает `AVAudioRecorder`.
    private func startRecordingIfNeeded() {
        guard !recorder.isRecording, !model.isParsing else { return }
        Task {
            if !micGranted {
                micGranted = await recorder.requestPermission()
            }
            guard micGranted else {
                model.alertMessage = "Нет доступа к микрофону. Разрешите его в Настройках, чтобы диктовать расход"
                return
            }
            guard !recorder.isRecording else { return }
            do {
                try recorder.start()
                Haptics.tap()
            } catch {
                model.alertMessage = error.localizedDescription
            }
        }
    }

    /// Отпустили микрофон: останавливаем запись и шлём аудио на распознавание.
    private func stopRecordingAndParse() {
        guard recorder.isRecording, let data = recorder.stop() else { return }
        Task {
            await model.parse(api: session.api, audio: data)
            recorder.reset()
        }
    }

    // MARK: Чек (позиции)

    /// Секция распознанного чека: карточка-чек с позициями, чипы переопределения
    /// деления, подсказки по нераспознанным именам и вопросам модели.
    private var receiptSection: some View {
        VStack(spacing: 14) {
            ReceiptCardView(
                model: model,
                meId: session.me?.id,
                onEditItem: { index in itemEditTarget = ItemEditTarget(index: index) },
                onResolveUnknown: { index, name in
                    unknownTarget = UnknownTarget(itemIndex: index, name: name)
                }
            )
            itemsOverrideChips
            if model.hasUnknownItems, let name = model.firstUnknownName {
                Label("Выберите, кто такой «\(name)» — коснитесь красной метки в чеке",
                      systemImage: "exclamationmark.triangle.fill")
                    .font(.system(size: 13, weight: .medium, design: .rounded))
                    .foregroundStyle(Color.negative)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            if !model.parseQuestions.isEmpty {
                ForEach(model.parseQuestions, id: \.self) { question in
                    Label(question, systemImage: "questionmark.circle")
                        .font(.system(size: 13, weight: .medium, design: .rounded))
                        .foregroundStyle(Color.inkSecondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
    }

    /// Чипы под чеком: «Поровну на всех» сбрасывает позиции (плоское деление),
    /// «По позициям» — текущее (выбранное) состояние.
    private var itemsOverrideChips: some View {
        HStack(spacing: 8) {
            Button {
                Haptics.tap()
                withAnimation(.spring(duration: 0.25)) { model.resetItems() }
            } label: {
                Text("Поровну на всех")
            }
            .buttonStyle(.softChip(isSelected: false))

            Button {} label: {
                Text("По позициям")
            }
            .buttonStyle(.softChip(isSelected: true))
            .allowsHitTesting(false)

            Spacer(minLength: 0)
        }
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
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(model.rooms) { room in
                            groupChip(room)
                        }
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
    }

    private func groupChip(_ room: RoomSummary) -> some View {
        Button {
            Haptics.tap()
            model.selectRoom(room)
        } label: {
            Text(room.name)
                .lineLimit(1)
        }
        .buttonStyle(.softChip(isSelected: room.id == model.selectedRoomId))
    }

    // MARK: Карточка «что и сколько»

    /// Описание сверху, hairline-разделитель, крупная сумма по центру.
    private func expenseCard(description: Binding<String>, sum: Binding<String>) -> some View {
        VStack(spacing: 16) {
            descriptionField(text: description)
            Rectangle()
                .fill(Color.hairline)
                .frame(height: 1)
            sumField(text: sum)
        }
        .surfaceCard()
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
                .font(.system(size: 19, weight: .medium, design: .rounded))
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
                .font(.system(size: 28, weight: .medium, design: .rounded))
                .foregroundStyle(Color.inkSecondary)
            TextField("0", text: text)
                .font(.system(size: 40, weight: .semibold, design: .rounded))
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

    // MARK: «Заплатили вы и разделено поровну / по суммам»

    private var splitCard: some View {
        VStack(spacing: 14) {
            splitModePicker
            splitSentence
            if model.splitType == .equally {
                if !model.members.isEmpty {
                    Text(model.splitHint)
                        .font(.system(size: 13, weight: .medium, design: .rounded))
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

    private func splitModeChip(_ title: String, type: SplitType) -> some View {
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
            segmentButton(model.splitType == .equally ? "поровну" : "по суммам") {
                isSplitPickerPresented = true
            }
        }
        .font(.system(size: 15, design: .rounded))
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
            Text(member.id == session.me?.id ? "\(member.displayName) (вы)" : member.displayName)
                .font(.system(size: 15, design: .rounded))
                .foregroundStyle(Color.ink)
                .lineLimit(1)
            Spacer(minLength: 8)
            TextField("0", text: amountBinding(for: member.id))
                .font(.system(size: 17, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .foregroundStyle(Color.ink)
                .multilineTextAlignment(.trailing)
                .keyboardType(.numberPad)
                .frame(width: 90)
                .focused($focusedField, equals: .amount(member.id))
            Text(currencySymbol(model.currency))
                .font(.system(size: 15, design: .rounded))
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
            .font(.system(size: 13, weight: .medium, design: .rounded))
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
            return "вы"
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
                        Text(member.id == meId ? "\(member.displayName) (вы)" : member.displayName)
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
                        Text(member.id == meId ? "\(member.displayName) (вы)" : member.displayName)
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
                    .font(.system(size: 15, weight: .medium, design: .rounded))
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

// MARK: - Карточка-чек (позиции)

/// Карточка распознанного чека: перфорированные края, пунктирные разделители,
/// моноширинные цифры (`MoneyText`), подвал Подытог→Сборы→Итого. Тап по позиции
/// открывает шит правки; ×N — неравные доли, замочек — фикс-сумма; красная метка
/// нераспознанного имени открывает сопоставление участнику.
private struct ReceiptCardView: View {
    let model: AddExpenseViewModel
    let meId: Int?
    let onEditItem: (Int) -> Void
    let onResolveUnknown: (Int, String) -> Void

    var body: some View {
        VStack(spacing: 0) {
            PerforationStrip(edge: .top)
            VStack(spacing: 0) {
                ForEach(Array(model.draftItemList.enumerated()), id: \.offset) { index, item in
                    if index > 0 {
                        DashedDivider()
                    }
                    itemRow(index: index, item: item)
                }
                DashedDivider()
                footer
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 4)
            .background(Color.surface)
            PerforationStrip(edge: .bottom)
        }
        .shadow(color: Color.black.opacity(0.06), radius: 14, x: 0, y: 6)
    }

    private func itemRow(index: Int, item: OperationItem) -> some View {
        Button {
            onEditItem(index)
        } label: {
            VStack(alignment: .leading, spacing: 6) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(item.name.isEmpty ? "Позиция" : item.name)
                        .font(.system(size: 15, weight: .medium, design: .rounded))
                        .foregroundStyle(Color.ink)
                    if item.qty > 1 {
                        Text("×\(item.qty)")
                            .font(.system(size: 12, weight: .semibold, design: .rounded))
                            .monospacedDigit()
                            .foregroundStyle(Color.inkSecondary)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.ink.opacity(0.06), in: Capsule())
                    }
                    Spacer(minLength: 8)
                    if item.shareList.contains(where: { $0.amount != nil }) {
                        Image(systemName: "lock.fill")
                            .font(.system(size: 11))
                            .foregroundStyle(Color.inkSecondary)
                    }
                    MoneyText(item.price, role: .neutral, size: 15, currency: model.currency)
                }
                participantsRow(index: index, item: item)
            }
            .padding(.vertical, 10)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    @ViewBuilder
    private func participantsRow(index: Int, item: OperationItem) -> some View {
        let unequal = hasUnequalWeights(item)
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                if item.isSurcharge {
                    Text(surchargeCaption(item))
                        .font(.system(size: 12, design: .rounded))
                        .foregroundStyle(Color.inkSecondary)
                } else {
                    ForEach(item.shareList, id: \.userId) { share in
                        shareChip(share: share, unequal: unequal)
                    }
                }
                ForEach(Array((item.unknown ?? []).enumerated()), id: \.offset) { _, name in
                    Button {
                        onResolveUnknown(index, name)
                    } label: {
                        Text(name)
                            .font(.system(size: 12, weight: .semibold, design: .rounded))
                            .foregroundStyle(Color.negative)
                            .padding(.horizontal, 8)
                            .padding(.vertical, 3)
                            .background(Color.negative.opacity(0.14), in: Capsule())
                            .overlay(Capsule().strokeBorder(Color.negative.opacity(0.5), lineWidth: 1))
                    }
                    .buttonStyle(.plain)
                }
            }
        }
    }

    private func shareChip(share: ItemShare, unequal: Bool) -> some View {
        HStack(spacing: 3) {
            if let user = member(share.userId) {
                UserAvatarView(user: user, size: 18)
            } else {
                Circle().fill(Color.inkSecondary.opacity(0.3)).frame(width: 18, height: 18)
            }
            if let amount = share.amount {
                Text(money(amount, currency: model.currency))
                    .font(.system(size: 11, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(Color.inkSecondary)
            } else if unequal, share.weight != 1 {
                Text("×\(share.weight)")
                    .font(.system(size: 11, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(Color.inkSecondary)
            }
        }
        .padding(.horizontal, 5)
        .padding(.vertical, 2)
        .background(Color.ink.opacity(0.05), in: Capsule())
    }

    private var footer: some View {
        VStack(spacing: 6) {
            footerLine("Подытог", model.itemizedSubtotal, bold: false)
            if model.itemizedSurcharges > 0 {
                footerLine("Сборы", model.itemizedSurcharges, bold: false)
            }
            footerLine("Итого", model.itemizedTotal ?? (model.itemizedSubtotal + model.itemizedSurcharges), bold: true)
        }
        .padding(.vertical, 8)
    }

    private func footerLine(_ title: String, _ amount: Int, bold: Bool) -> some View {
        HStack {
            Text(title)
                .font(.system(size: bold ? 15 : 13, weight: bold ? .semibold : .medium, design: .rounded))
                .foregroundStyle(bold ? Color.ink : Color.inkSecondary)
            Spacer()
            MoneyText(amount, role: .neutral, size: bold ? 17 : 13, currency: model.currency)
        }
    }

    private func member(_ id: Int) -> User? {
        model.members.first { $0.id == id }
    }

    private func hasUnequalWeights(_ item: OperationItem) -> Bool {
        let weights = item.shareList.filter { $0.amount == nil }.map(\.weight)
        guard let first = weights.first else { return false }
        return weights.contains { $0 != first }
    }

    private func surchargeCaption(_ item: OperationItem) -> String {
        let rule = item.split == OperationItem.splitEqually ? "поровну" : "пропорционально"
        if let pct = item.percent {
            return "Сбор \(pct)% · \(rule)"
        }
        return "Сбор · \(rule)"
    }
}

/// Пунктирный разделитель строк чека.
private struct DashedDivider: View {
    var body: some View {
        DashedLine()
            .stroke(Color.hairline, style: StrokeStyle(lineWidth: 1, dash: [4, 4]))
            .frame(height: 1)
            .padding(.vertical, 8)
    }
}

private struct DashedLine: Shape {
    func path(in rect: CGRect) -> Path {
        var path = Path()
        path.move(to: CGPoint(x: 0, y: rect.midY))
        path.addLine(to: CGPoint(x: rect.maxX, y: rect.midY))
        return path
    }
}

/// Перфорированный (зубчатый) край чека: ряд полукругов цвета фона, надрезающих
/// поверхность карточки. Тема-независим: круги — `Color.bg`, полоса — `Color.surface`.
private struct PerforationStrip: View {
    enum Edge { case top, bottom }
    let edge: Edge

    var body: some View {
        let diameter: CGFloat = 10
        GeometryReader { geo in
            let count = max(1, Int(geo.size.width / diameter))
            HStack(spacing: 0) {
                ForEach(0..<count, id: \.self) { _ in
                    Circle()
                        .fill(Color.bg)
                        .frame(width: diameter, height: diameter)
                }
            }
            .frame(width: geo.size.width, alignment: .center)
            .offset(y: edge == .top ? -diameter / 2 : diameter / 2)
        }
        .frame(height: diameter / 2)
        .background(Color.surface)
        .clipped()
    }
}

// MARK: - Шит правки позиции

/// Шит правки одной позиции чека: название и цена; переключатель «Долями /
/// Суммами» (ОДИН контрол на строку — степпер веса ИЛИ поле суммы); участие по
/// тапу на имя; пустое поле суммы = «авто» (доля по весу). У надбавки правится
/// только название/цена (делится по базе, а не по своим долям).
private struct ItemSheetView: View {
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
    private let isSurcharge: Bool
    private let originalItem: OperationItem?

    init(model: AddExpenseViewModel, index: Int, meId: Int?) {
        self.model = model
        self.index = index
        self.meId = meId
        let item = model.draftItemList.indices.contains(index) ? model.draftItemList[index] : nil
        self.originalItem = item
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

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 16) {
                    fieldsCard
                    if !isSurcharge {
                        modePicker
                        participantsCard
                    }
                }
                .padding(20)
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
                }
            }
        }
        .tint(Color.accent)
    }

    private var fieldsCard: some View {
        VStack(spacing: 12) {
            TextField("Название", text: $name)
                .font(.system(size: 17, weight: .medium, design: .rounded))
                .foregroundStyle(Color.ink)
            Rectangle().fill(Color.hairline).frame(height: 1)
            HStack {
                Text("Цена")
                    .font(.system(size: 15, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
                Spacer()
                TextField("0", text: priceBinding)
                    .font(.system(size: 17, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .multilineTextAlignment(.trailing)
                    .keyboardType(.numberPad)
                    .frame(width: 100)
                Text(currencySymbol(model.currency))
                    .font(.system(size: 15, design: .rounded))
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
                Text(member.id == meId ? "\(member.displayName) (вы)" : member.displayName)
                    .font(.system(size: 15, design: .rounded))
                    .foregroundStyle(isOn ? Color.ink : Color.inkSecondary)
                    .lineLimit(1)
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

    /// Ровно ОДИН контрол на строку: степпер веса («Долями») ИЛИ поле суммы
    /// («Суммами», пустое = «авто»).
    @ViewBuilder
    private func control(_ userId: Int) -> some View {
        if byAmount {
            HStack(spacing: 4) {
                TextField("авто", text: amountBinding(userId))
                    .font(.system(size: 15, weight: .semibold, design: .rounded))
                    .monospacedDigit()
                    .multilineTextAlignment(.trailing)
                    .keyboardType(.numberPad)
                    .frame(width: 72)
                Text(currencySymbol(model.currency))
                    .font(.system(size: 13, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
            }
        } else {
            Stepper("×\(weights[userId] ?? 1)", value: weightBinding(userId), in: 1...20)
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
        guard let original = originalItem else { return }
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
        model.replaceItem(at: index, with: OperationItem(
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
            .navigationTitle("Кто такой «\(name)»?")
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
