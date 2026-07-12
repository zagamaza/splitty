import SwiftUI

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
                expenseCard(description: $model.descriptionText, sum: $model.sumText)
                splitCard
                if model.isEditingLocalEntry {
                    deleteLocalButton
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 16)
            .padding(.bottom, 24)
        }
        .scrollDismissesKeyboard(.interactively)
        .safeAreaInset(edge: .bottom) {
            saveButton
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

    /// Нижний CTA «Сохранить»: premium pill вместо тулбарной кнопки.
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
        // офлайн-правка синхронизированной операции заблокирована.
        .disabled(model.isSaving || !model.canSave || isOfflineBlocked)
        .padding(.horizontal, 20)
        .padding(.vertical, 8)
        .background(Color.bg)
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

#Preview {
    AddExpenseView()
        .environment(SessionStore())
}
