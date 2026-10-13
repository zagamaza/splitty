import SwiftUI
import UIKit

/// Карточка операции — один документ-чек: дата и название, сумма и кто
/// платил, доли, позиции чека и вложения разделами того же листа. Под листом
/// «Изменить»/«Удалить» — доступны любому участнику комнаты (Splitwise-
/// семантика, сервер разрешает участникам).
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
                document
                // Редактировать/удалять может любой участник комнаты
                // (Splitwise-семантика, сервер разрешает участникам).
                actionsSection
            }
            .padding(16)
        }
        .background(Color.bg)
        .navigationTitle(operation.isDebtRepayment ? "Погашение" : "Расход")
        .trackScreen("operation")
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

    // MARK: Документ

    /// Один лист: шапка, сумма и плательщик, доли, позиции, вложения —
    /// разделы через пунктир, без отдельных карточек.
    private var document: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            sectionBreak
            amountBlock
            sectionBreak
            recipientsBlock
            if !operation.itemList.isEmpty {
                sectionBreak
                itemsBlock(operation.itemList)
            }
            if let files = operation.files, !files.isEmpty {
                sectionBreak
                filesBlock(files)
            }
        }
        .receiptPaper(padding: 22)
    }

    private var sectionBreak: some View {
        ReceiptDivider().padding(.vertical, 16)
    }

    /// Шапка документа: тип и дата реквизитом, название. Погашение — другой
    /// документ, а не расход: это видно с первой строки.
    private var header: some View {
        let date = Self.fullDate.string(from: operation.createdAt)
        return VStack(spacing: 8) {
            Text(operation.isDebtRepayment ? "Погашение · \(date)" : "Расход · \(date)").receiptLabel()
            Text(title)
                .scaledFont(size: 24, weight: .heavy, relativeTo: .title2)
                .kerning(-0.6)
                .foregroundStyle(Color.ink)
                .multilineTextAlignment(.center)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    private var title: String {
        if operation.isDebtRepayment {
            return String(localized: "Погашение долга")
        }
        return operation.description.isEmpty ? String(localized: "Расход") : operation.description
    }

    /// Сумма крупно и строкой — кто платил. Роль словами в подписи, имя без
    /// глагола: «заплатил(а)» не нужно гадать.
    private var amountBlock: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Сумма").receiptLabel()
                // Без fixedSize: на узком экране с крупным шрифтом сумма
                // ужимается в ширину листа, а не вылезает за него.
                MoneyText(operation.sum, exactMinor: operation.exactMinor, role: .neutral,
                          size: 40, weight: .heavy, currency: currency)
            }
            .accessibilityElement(children: .combine)
            documentRow(
                Text(operation.isDebtRepayment ? "Кто отправил" : "Кто платил"),
                value: Text(name(operation.donor))
            )
        }
    }

    /// Доли — ХРАНИМЫЕ суммы операции (`recipients[].sum`): при «по суммам»
    /// именно они, а не пересчёт поровну.
    private var recipientsBlock: some View {
        VStack(alignment: .leading, spacing: 4) {
            ViewThatFits(in: .horizontal) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    recipientsLabel.fixedSize()
                    splitLabel.fixedSize()
                }
                VStack(alignment: .leading, spacing: 2) {
                    recipientsLabel
                    splitLabel
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
            .padding(.bottom, 4)
            ForEach(operation.recipients) { recipient in
                recipientRow(recipient)
            }
        }
    }

    /// Имя слева, доля справа; длинное имя на крупном шрифте не рвётся
    /// посреди слова — тогда доля уходит под имя.
    private func recipientRow(_ recipient: OperationRecipient) -> some View {
        let who = VStack(alignment: .leading, spacing: 2) {
            Text(name(recipient.user))
                .scaledFont(size: 16)
                .foregroundStyle(Color.ink)
                .fixedSize(horizontal: false, vertical: true)
            if let caption = recipientCaption(recipient.user) {
                Text(caption)
                    .scaledFont(size: 12, relativeTo: .caption)
                    .foregroundStyle(Color.inkSecondary)
            }
        }
        let amount = MoneyText(recipient.sum, exactMinor: recipient.exactMinor, role: .neutral,
                               size: 16, weight: .semibold, currency: currency)
        return ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                who.fixedSize()
                Spacer(minLength: 8)
                amount.fixedSize()
            }
            VStack(alignment: .leading, spacing: 2) {
                who
                amount
            }
        }
        .padding(.vertical, 6)
        .accessibilityElement(children: .combine)
    }

    private var recipientsLabel: some View {
        Text(operation.isDebtRepayment ? "Кто получил" : "Кто участвует").receiptLabel()
    }

    @ViewBuilder
    private var splitLabel: some View {
        if !operation.isDebtRepayment {
            Text(operation.splitType == .byExactAmount ? "· по суммам" : "· поровну")
                .scaledFont(size: 12, relativeTo: .footnote)
                .foregroundStyle(Color.inkSecondary)
        }
    }

    private func name(_ user: User) -> String {
        user.id == currentUserId ? String(localized: "Вы") : user.displayName
    }

    /// Подпись доли — «ваша доля» / «доля» у всех, включая плательщика.
    /// Сумма — историческая доля операции (`recipients[].sum`), а не текущий
    /// долг: после расчёта «ваш долг 3000» под старым расходом врал бы.
    /// Текущий долг честно показан в группе и на «Балансах». У погашения
    /// подпись не нужна: раздел и так называется «Кто получил».
    private func recipientCaption(_ recipient: User) -> String? {
        if operation.isDebtRepayment { return nil }
        return recipient.id == currentUserId ? String(localized: "ваша доля") : String(localized: "доля")
    }

    /// Строка документа: подпись слева, значение справа; на крупном шрифте —
    /// значение под подписью.
    private func documentRow(_ title: Text, value: Text) -> some View {
        let label = title
            .scaledFont(size: 15)
            .foregroundStyle(Color.inkSecondary)
        let content = value
            .scaledFont(size: 16, weight: .semibold)
            .foregroundStyle(Color.ink)
        return ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline) {
                label.fixedSize()
                Spacer(minLength: 8)
                content.fixedSize()
            }
            VStack(alignment: .leading, spacing: 2) {
                label.fixedSize(horizontal: false, vertical: true)
                content.fixedSize(horizontal: false, vertical: true)
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// Позиции чека (itemized-операция, AI-распознавание) — только чтение,
    /// разделом того же листа: своя шапка «Позиции · N поз.» и подвал Итого.
    private func itemsBlock(_ items: [OperationItem]) -> some View {
        var members = operation.recipients.map(\.user)
        if !members.contains(where: { $0.id == operation.donor.id }) {
            members.append(operation.donor)
        }
        return ReceiptView(items: items, members: members, currency: currency, embedded: true)
    }

    private func filesBlock(_ files: [OperationFile]) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Вложения").receiptLabel()
                .accessibilityAddTraits(.isHeader)
            ForEach(files) { file in
                Button {
                    previewFile = file
                } label: {
                    HStack {
                        Label(attachmentTypeName(file.type), systemImage: "paperclip")
                            .scaledFont(size: 16)
                            .foregroundStyle(Color.ink)
                        Spacer()
                        Image(systemName: "chevron.right")
                            .font(.caption.weight(.semibold))
                            .foregroundStyle(Color.inkSecondary.opacity(0.6))
                    }
                    .frame(minHeight: 44)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
    }

    /// Под листом: «Изменить» рамкой (у погашений его нет) и «Удалить»
    /// красной ссылкой — разрушительное действие тише основного.
    private var actionsSection: some View {
        VStack(spacing: 4) {
            if !operation.isDebtRepayment {
                Button("Изменить") { isEditPresented = true }
                    .buttonStyle(.receiptOutline)
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
                        Text("Удалить")
                            .underline(true, color: Color.negativeText.opacity(0.6))
                    }
                }
                .scaledFont(size: 15, weight: .medium)
                .foregroundStyle(Color.negativeText)
                .frame(maxWidth: .infinity, minHeight: 44)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(isDeleting)
        }
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
