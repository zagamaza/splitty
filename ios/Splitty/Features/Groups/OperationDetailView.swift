import SwiftUI
import UIKit

/// Карточка операции: hero-сумма, участники с аватарами и долями,
/// файл-чек, «Изменить»/«Удалить» — только если текущий пользователь — donor.
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


    private static let fullDate: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "ru_RU")
        formatter.dateFormat = "d MMMM yyyy"
        return formatter
    }()

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                headerCard
                participantsSection
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
            Text("Операция исчезнет из группы, балансы пересчитаются.")
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
                            .font(.system(size: 22))
                            .foregroundStyle(operation.isDebtRepayment ? Color.accent : Color.inkSecondary)
                    }
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                        .font(.system(size: 17, weight: .semibold, design: .rounded))
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
            return "Погашение долга"
        }
        return operation.description.isEmpty ? "Расход" : operation.description
    }

    /// «Детали»: плательщик и получатели с аватарами и долями.
    /// Доли — ХРАНИМЫЕ суммы операции (`recipients[].sum`): при делении
    /// «по суммам» именно они, а не пересчёт поровну.
    private var participantsSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text("Детали")
                    .sectionHeaderStyle()
                if !operation.isDebtRepayment {
                    Text(operation.splitType == .byExactAmount ? "· по суммам" : "· поровну")
                        .font(.system(size: 12, design: .rounded))
                        .foregroundStyle(Color.inkSecondary.opacity(0.7))
                }
            }
            .padding(.leading, 4)
            VStack(spacing: 0) {
                donorRow
                ForEach(operation.recipients) { recipient in
                    Rectangle()
                        .fill(Color.hairline)
                        .frame(height: 1)
                        .padding(.leading, 64)
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
                Text(operation.donor.id == currentUserId ? "Вы" : operation.donor.displayName)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.ink)
                Text(operation.donor.id == currentUserId ? "заплатили" : "заплатил(а)")
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
            UserAvatarView(user: recipient.user, size: 32)
                .padding(.leading, 4)
            VStack(alignment: .leading, spacing: 2) {
                Text(recipient.user.id == currentUserId ? "Вы" : recipient.user.displayName)
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
            return recipient.id == currentUserId ? "получили" : "получил(а)"
        }
        if recipient.id == operation.donor.id {
            return recipient.id == currentUserId ? "ваша доля" : "доля"
        }
        if recipient.id == currentUserId {
            return "вы должны"
        }
        return "должен(на)"
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
                            Label(
                                file.type == "image" ? "Фото (чек)" : "Вложение (\(file.type))",
                                systemImage: "paperclip"
                            )
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
                        .foregroundStyle(Color.accent)
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
                .foregroundStyle(Color.negative)
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
            alertMessage = error.localizedDescription
        }
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
    @State private var isFailed = false
    @State private var alertMessage: String?

    var body: some View {
        NavigationStack {
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.bg)
                .navigationTitle(file.type == "image" ? "Фото (чек)" : "Вложение")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Готово") { dismiss() }
                    }
                }
                .task { await load() }
                .alert("Ошибка", isPresented: alertPresented) {
                    Button("Ок", role: .cancel) {}
                } message: {
                    Text(alertMessage ?? "")
                }
        }
    }

    private var alertPresented: Binding<Bool> {
        Binding(
            get: { alertMessage != nil },
            set: { if !$0 { alertMessage = nil } }
        )
    }

    @ViewBuilder
    private var content: some View {
        if let image {
            Image(uiImage: image)
                .resizable()
                .scaledToFit()
        } else if let tempFileURL {
            VStack(spacing: 24) {
                Image(systemName: file.type == "video" ? "video" : "doc")
                    .font(.system(size: 44))
                    .foregroundStyle(Color.inkSecondary)
                ShareLink(item: tempFileURL) {
                    Label("Открыть / поделиться", systemImage: "square.and.arrow.up")
                }
                .buttonStyle(.primaryPill)
                .padding(.horizontal, 40)
            }
        } else if isFailed {
            ContentUnavailableView {
                Label("Не удалось загрузить", systemImage: "wifi.exclamationmark")
            } actions: {
                Button("Повторить") {
                    Task { await load() }
                }
            }
        } else {
            ProgressView()
        }
    }

    private func load() async {
        isFailed = false
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
            isFailed = true
            alertMessage = error.localizedDescription
        }
    }
}
