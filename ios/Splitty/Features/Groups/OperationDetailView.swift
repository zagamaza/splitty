import SwiftUI
import UIKit

/// Карточка операции: hero-сумма, участники с аватарами и долями,
/// файл-чек, «Изменить»/«Удалить» — доступны любому участнику комнаты
/// (Splitwise-семантика, сервер разрешает участникам).
struct OperationDetailView: View {
    private let roomId: String
    private let currentUserId: Int
    /// Валюта комнаты — в ней все суммы карточки.
    private let currency: String
    private let onChange: () -> Void

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var operation: Operation
    @State private var isEditPresented = false
    @State private var isDeleteConfirmPresented = false
    @State private var isDeleting = false
    @State private var alertMessage: String?
    /// Вложение, открытое на просмотр (sheet).
    @State private var previewFile: OperationFile?

    init(
        roomId: String,
        operation: Operation,
        currentUserId: Int,
        currency: String = "RUB",
        onChange: @escaping () -> Void
    ) {
        self.roomId = roomId
        self.currentUserId = currentUserId
        self.currency = currency
        self.onChange = onChange
        _operation = State(initialValue: operation)
    }


    /// Локаль текущая, а не русская: приложение переведено на пять языков, и
    /// человек, выбравший английский, видел здесь дату по-русски
    private static var fullDate: DateFormatter {
        let formatter = DateFormatter()
        formatter.locale = DateFmt.locale
        formatter.setLocalizedDateFormatFromTemplate("d MMMM yyyy")
        return formatter
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                headerCard
                participantsSection
                if !operation.itemList.isEmpty {
                    itemsSection(operation.itemList)
                }
                if let files = operation.files, !files.isEmpty {
                    filesSection(files)
                }
                // Редактировать/удалять может любой участник комнаты
                // (Splitwise-семантика, сервер разрешает участникам).
                actionsSection
            }
            .padding(16)
        }
        .background(Color.bg)
        .navigationTitle(operation.isDebtRepayment ? "Погашение" : "Расход")
        .navigationBarTitleDisplayMode(.inline)
        // Полноэкранно, а не sheet: форму со введёнными данными нельзя
        // случайно смахнуть — выход только «Отмена»/«Сохранить».
        .fullScreenCover(isPresented: $isEditPresented) {
            AddExpenseView(roomId: roomId, editOperation: operation) {
                onChange()
                Task { await reloadOperation() }
            }
        }
        .sheet(item: $previewFile) { file in
            OperationFileView(file: file)
        }
        .confirmationDialog(
            operation.isDebtRepayment ? "Удалить платёж?" : "Удалить расход?",
            isPresented: $isDeleteConfirmPresented,
            titleVisibility: .visible
        ) {
            Button("Удалить", role: .destructive) {
                Task { await deleteOperation() }
            }
            Button("Отмена", role: .cancel) {}
        } message: {
            // У погашений нет «Изменить» — объясняем, как исправить ошибку.
            Text(
                operation.isDebtRepayment
                    ? "Операция исчезнет из группы, балансы пересчитаются. Погашения не редактируются — при ошибке удалите и запишите заново."
                    : "Операция исчезнет из группы, балансы пересчитаются."
            )
        }
        .alert("Ошибка", isPresented: alertPresented) {
            Button("Ок", role: .cancel) {}
        } message: {
            Text(alertMessage ?? "")
        }
    }

    private var alertPresented: Binding<Bool> {
        Binding(
            get: { alertMessage != nil },
            set: { if !$0 { alertMessage = nil } }
        )
    }

    // MARK: Секции

    /// Hero-карточка: иконка, описание, крупная сумма, дата.
    private var headerCard: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 12) {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(operation.isDebtRepayment ? Color.accent.opacity(0.14) : Color.ink.opacity(0.06))
                    .frame(width: 48, height: 48)
                    .overlay {
                        Image(systemName: operation.isDebtRepayment ? "banknote" : "doc.plaintext")
                            .scaledFont(size: 22, design: .default)
                            .foregroundStyle(operation.isDebtRepayment ? Color.accent : Color.inkSecondary)
                    }
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .scaledFont(size: 17, weight: .semibold)
                        .foregroundStyle(Color.ink)
                    Text("Добавлено \(Self.fullDate.string(from: operation.createdAt))")
                        .font(.caption)
                        .foregroundStyle(Color.inkSecondary)
                }
            }
            MoneyText(operation.sum, role: .neutral, size: 40, currency: currency)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard(padding: 20)
    }

    private var title: String {
        if operation.isDebtRepayment {
            return String(localized: "Погашение долга")
        }
        return operation.description.isEmpty ? String(localized: "Расход") : operation.description
    }

    /// Две раздельные секции вместо сплошного списка: «Кто платил» (донор с
    /// полной суммой) и «Кто участвует» (получатели с долями). Доли — ХРАНИМЫЕ
    /// суммы операции (`recipients[].sum`): при «по суммам» именно они.
    private var participantsSection: some View {
        VStack(alignment: .leading, spacing: 16) {
            payerSection
            recipientsSection
        }
    }

    private var payerSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(operation.isDebtRepayment ? "Кто отправил" : "Кто платил")
                .sectionHeaderStyle()
                .padding(.leading, 4)
            donorRow
                .surfaceCard(padding: 0)
        }
    }

    private var recipientsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(operation.isDebtRepayment ? "Кто получил" : "Кто участвует")
                    .sectionHeaderStyle()
                if !operation.isDebtRepayment {
                    Text(operation.splitType == .byExactAmount ? "· по суммам" : "· поровну")
                        .scaledFont(size: 12, relativeTo: .footnote)
                        .foregroundStyle(Color.inkSecondary.opacity(0.7))
                }
            }
            .padding(.leading, 4)
            VStack(spacing: 0) {
                ForEach(operation.recipients) { recipient in
                    if recipient.id != operation.recipients.first?.id {
                        Rectangle()
                            .fill(Color.hairline)
                            .frame(height: 1)
                            .padding(.leading, 64)
                    }
                    recipientRow(recipient)
                }
            }
            .surfaceCard(padding: 0)
        }
    }

    private var donorRow: some View {
        HStack(spacing: 12) {
            UserAvatarView(user: operation.donor, size: 36)
            VStack(alignment: .leading, spacing: 2) {
                Text(operation.donor.id == currentUserId ? String(localized: "Вы") : operation.donor.displayName)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.ink)
                Text(operation.donor.id == currentUserId ? "заплатили за всех" : "заплатил(а) за всех")
                    .font(.caption)
                    .foregroundStyle(Color.inkSecondary)
            }
            Spacer(minLength: 8)
            MoneyText(operation.sum, role: .neutral, size: 15, currency: currency)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
    }

    private func recipientRow(_ recipient: OperationRecipient) -> some View {
        HStack(spacing: 12) {
            UserAvatarView(user: recipient.user, size: 36)
            VStack(alignment: .leading, spacing: 2) {
                Text(recipient.user.id == currentUserId ? String(localized: "Вы") : recipient.user.displayName)
                    .font(.subheadline)
                    .foregroundStyle(Color.ink)
                Text(recipientCaption(recipient.user))
                    .font(.caption)
                    .foregroundStyle(Color.inkSecondary)
            }
            Spacer(minLength: 8)
            MoneyText(recipient.sum, role: recipientRole(recipient.user), size: 15, currency: currency)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
    }

    /// Подпись позиции получателя (словоформы — как в боте).
    private func recipientCaption(_ recipient: User) -> String {
        if operation.isDebtRepayment {
            return recipient.id == currentUserId ? String(localized: "получили") : String(localized: "получил(а)")
        }
        if recipient.id == operation.donor.id {
            return recipient.id == currentUserId ? String(localized: "ваша доля") : String(localized: "доля")
        }
        if recipient.id == currentUserId {
            return String(localized: "вы должны")
        }
        return String(localized: "должен(на)")
    }

    /// Цвет доли: негатив — только для СВОЕГО долга; остальное нейтрально.
    private func recipientRole(_ recipient: User) -> MoneyText.Role {
        if !operation.isDebtRepayment,
           recipient.id == currentUserId,
           recipient.id != operation.donor.id {
            return .negative
        }
        return .neutral
    }

    /// Позиции чека (itemized-операция, AI-распознавание) — только чтение:
    /// название, количество, участники и цена; подвал Подытог→Сборы→Итого.
    private func itemsSection(_ items: [OperationItem]) -> some View {
        var members = operation.recipients.map(\.user)
        if !members.contains(where: { $0.id == operation.donor.id }) {
            members.append(operation.donor)
        }
        return VStack(alignment: .leading, spacing: 10) {
            Text("Позиции чека")
                .sectionHeaderStyle()
                .padding(.leading, 4)
            ReceiptView(items: items, members: members, currency: currency)
        }
    }

    private func filesSection(_ files: [OperationFile]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Вложения")
                .sectionHeaderStyle()
                .padding(.leading, 4)
            VStack(spacing: 0) {
                ForEach(files) { file in
                    Button {
                        previewFile = file
                    } label: {
                        HStack {
                            Label(attachmentTypeName(file.type), systemImage: "paperclip")
                            .font(.subheadline)
                            .foregroundStyle(Color.ink)
                            Spacer()
                            Image(systemName: "chevron.right")
                                .font(.caption.weight(.semibold))
                                .foregroundStyle(Color.inkSecondary.opacity(0.6))
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 14)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    if file.id != files.last?.id {
                        Rectangle()
                            .fill(Color.hairline)
                            .frame(height: 1)
                            .padding(.leading, 16)
                    }
                }
            }
            .surfaceCard(padding: 0)
        }
    }

    private var actionsSection: some View {
        VStack(spacing: 0) {
            if !operation.isDebtRepayment {
                Button {
                    isEditPresented = true
                } label: {
                    Label("Изменить", systemImage: "pencil")
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(Color.accentText)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 14)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                Rectangle()
                    .fill(Color.hairline)
                    .frame(height: 1)
                    .padding(.leading, 16)
            }
            Button(role: .destructive) {
                isDeleteConfirmPresented = true
            } label: {
                Group {
                    if isDeleting {
                        HStack {
                            ProgressView()
                            Text("Удаление…")
                        }
                    } else {
                        Label("Удалить", systemImage: "trash")
                    }
                }
                .font(.subheadline.weight(.medium))
                .foregroundStyle(Color.negativeText)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 16)
                .padding(.vertical, 14)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(isDeleting)
        }
        .surfaceCard(padding: 0)
    }

    // MARK: Действия

    /// После редактирования подтягивает свежую версию операции.
    private func reloadOperation() async {
        do {
            let operations = try await session.api.operations(roomId: roomId, type: "all")
            if let updated = operations.first(where: { $0.id == operation.id }) {
                operation = updated
            }
        } catch {
            // Не критично: карточка останется со старыми данными, список обновится.
        }
    }

    private func deleteOperation() async {
        isDeleting = true
        defer { isDeleting = false }
        do {
            try await session.api.deleteOperation(roomId: roomId, operationId: operation.id)
            // Единая инвалидация: экран группы и списки перезагрузятся по dataVersion.
            session.noteDataChanged()
            Haptics.success()
            onChange()
            dismiss()
        } catch {
            alertMessage = humanErrorText(error)
        }
    }
}

// MARK: - Тип вложения

/// Человеческое имя типа вложения: сырое значение API («video») в UI пугает.
private func attachmentTypeName(_ type: String) -> String {
    switch type {
    case "photo", "image": return String(localized: "Фото")
    case "video": return String(localized: "Видео")
    case "document": return String(localized: "Документ")
    default: return String(localized: "Вложение")
    }
}

// MARK: - Просмотр вложения

/// Просмотр файла операции: скачивает GET /files/{fileId};
/// фото показывает картинкой, видео/прочее — ShareLink по временному файлу.
private struct OperationFileView: View {
    let file: OperationFile

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var image: UIImage?
    @State private var tempFileURL: URL?
    /// Текст ошибки загрузки: показывается ТОЛЬКО failed-state'ом
    /// (alert поверх него дублировал ту же ошибку двумя окнами).
    @State private var loadErrorText: String?

    var body: some View {
        NavigationStack {
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.bg)
                .navigationTitle(attachmentTypeName(file.type))
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Готово") { dismiss() }
                    }
                }
                .task { await load() }
        }
    }

    @ViewBuilder
    private var content: some View {
        if let image {
            // Чеки читают приближая: без зума мелкий текст бесполезен.
            ZoomableImage(image: image)
        } else if let tempFileURL {
            VStack(spacing: 24) {
                Image(systemName: file.type == "video" ? "video" : "doc")
                    .scaledFont(size: 44, design: .default, relativeTo: .title)
                    .foregroundStyle(Color.inkSecondary)
                ShareLink(item: tempFileURL) {
                    Label("Открыть / поделиться", systemImage: "square.and.arrow.up")
                }
                .buttonStyle(.primaryPill)
                .padding(.horizontal, 40)
            }
        } else if let loadErrorText {
            FailedStateView(message: loadErrorText) {
                await load()
            }
        } else {
            ProgressView()
        }
    }

    private func load() async {
        loadErrorText = nil
        do {
            let data = try await session.api.fileData(id: file.fileId)
            if file.type == "image", let uiImage = UIImage(data: data) {
                image = uiImage
                return
            }
            // Видео/прочее (или нераспознанное фото): во временный файл для ShareLink.
            let ext = file.type == "video" ? "mp4" : "dat"
            let url = FileManager.default.temporaryDirectory
                .appendingPathComponent(UUID().uuidString)
                .appendingPathExtension(ext)
            try data.write(to: url)
            tempFileURL = url
        } catch {
            // Отмена .task (закрыли sheet) — не ошибка.
            if error.isTaskCancellation { return }
            loadErrorText = humanErrorText(error)
        }
    }
}

// MARK: - Зумируемое фото

/// Фото с pinch-зумом (1×…4×) и перетаскиванием: чеки читают приближая,
/// в plain `scaledToFit` мелкие строки нечитаемы. Double-tap — сброс к 1×.
private struct ZoomableImage: View {
    let image: UIImage

    /// Зафиксированные значения после завершения жеста.
    @State private var steadyScale: CGFloat = 1
    @State private var steadyOffset: CGSize = .zero
    /// «Живые» значения во время жеста.
    @State private var scale: CGFloat = 1
    @State private var offset: CGSize = .zero

    /// Пределы масштаба: 1× (исходный) … 4× (читаемость мелкого текста).
    private static let scaleRange: ClosedRange<CGFloat> = 1...4

    var body: some View {
        Image(uiImage: image)
            .resizable()
            .scaledToFit()
            .scaleEffect(scale)
            .offset(offset)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .contentShape(Rectangle())
            .gesture(magnify.simultaneously(with: drag))
            .onTapGesture(count: 2) { resetZoom() }
            // Увеличенное фото не должно вылезать под навбар и края экрана.
            .clipped()
    }

    private var magnify: some Gesture {
        MagnifyGesture()
            .onChanged { value in
                scale = Self.clamped(steadyScale * value.magnification)
            }
            .onEnded { value in
                steadyScale = Self.clamped(steadyScale * value.magnification)
                scale = steadyScale
                // Возврат к 1× сбрасывает и смещение — иначе фото «уезжает».
                if steadyScale == 1 { resetZoom() }
            }
    }

    private var drag: some Gesture {
        DragGesture()
            .onChanged { value in
                // Двигать имеет смысл только увеличенное фото.
                guard steadyScale > 1 else { return }
                offset = CGSize(
                    width: steadyOffset.width + value.translation.width,
                    height: steadyOffset.height + value.translation.height
                )
            }
            .onEnded { _ in
                steadyOffset = offset
            }
    }

    private func resetZoom() {
        withAnimation(.spring(duration: 0.3)) {
            scale = 1
            offset = .zero
        }
        steadyScale = 1
        steadyOffset = .zero
    }

    private static func clamped(_ value: CGFloat) -> CGFloat {
        min(max(value, scaleRange.lowerBound), scaleRange.upperBound)
    }
}

// MARK: - Карточка операции по id (тап по push)

/// Карточка операции, открытая по push: в payload есть только id, а сама
/// операция живёт в детали комнаты — отдельного GET операции в API нет
/// (как и на Android, см. `OperationDetailViewModel`).
///
/// Три исхода вместо одного экрана ошибки: операции нет в комнате (её удалили,
/// пока уведомление лежало в шторке), комната не читается вовсе (вышли из неё —
/// сервер отвечает 403) и «пока грузим». Ни один из них не должен выглядеть как
/// пустая карточка: под этим экраном лежит группа, «назад» уводит из тупика.
struct PushOperationView: View {
    let roomId: String
    let operationId: String

    @Environment(SessionStore.self) private var session
    @State private var operation: Operation?
    @State private var currency = "RUB"
    @State private var isMissing = false
    @State private var loadErrorText: String?

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.bg)
            .task {
                // Профиль мог не загрузиться на старте (холодный старт по тапу
                // приходит раньше первого запроса) — без meId доли не подписать.
                if session.me == nil {
                    await session.refreshMe()
                }
                await load()
            }
    }

    @ViewBuilder
    private var content: some View {
        if let operation, let meId = session.me?.id {
            OperationDetailView(
                roomId: roomId,
                operation: operation,
                currentUserId: meId,
                currency: currency
            ) {}
        } else if isMissing {
            ContentUnavailableView {
                Label("Операция не найдена", systemImage: "doc.questionmark")
            } description: {
                Text("Её могли удалить, пока уведомление ждало в шторке")
            }
            .navigationTitle("Расход")
            .navigationBarTitleDisplayMode(.inline)
        } else if let loadErrorText {
            FailedStateView(message: loadErrorText) {
                await load()
            }
        } else {
            ProgressView()
        }
    }

    private func load() async {
        isMissing = false
        loadErrorText = nil
        do {
            let room = try await session.repo.room(id: roomId).value
            currency = room.currency
            // Операции нет — это не ошибка загрузки: повторять запрос незачем,
            // и «Повторить» здесь только обманывало бы.
            guard let found = room.operations.first(where: { $0.id == operationId }) else {
                isMissing = true
                return
            }
            operation = found
        } catch {
            // Отмена .task (ушли с экрана) — не ошибка.
            if error.isTaskCancellation { return }
            loadErrorText = humanErrorText(error)
        }
    }
}
