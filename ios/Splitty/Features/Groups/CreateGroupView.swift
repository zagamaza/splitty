import SwiftUI

/// Создание группы: поле «Название», выбор валюты, CTA «Создать».
struct CreateGroupView: View {
    /// Валюта последней тусы человека — с неё начинается подстановка.
    /// nil — вызывающий её не знает (например, экран «Друзья»).
    private let recentCurrency: String?

    /// Созданная группа отдаётся вызвавшему экрану: в списке групп она видна
    /// сразу, а с «Друзей» без этого не менялось ничего — группу создавали по
    /// нескольку раз, не понимая, сработало ли.
    private let onCreated: (RoomDetail) -> Void

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var isSaving = false
    @State private var alertMessage: String?
    @FocusState private var isNameFocused: Bool

    /// Справочник валют; nil — ещё не пришёл или не пришёл вовсе.
    @State private var currencies: [CurrencyInfo]?
    @State private var selectedCurrency: String?
    /// Человек сменил подставленную валюту руками — уходит в аналитику:
    /// без этого не ответить, нужен ли выбор на этом экране вообще.
    @State private var didPickCurrency = false

    init(recentCurrency: String? = nil, onCreated: @escaping (RoomDetail) -> Void) {
        self.recentCurrency = recentCurrency
        self.onCreated = onCreated
    }

    private var trimmedName: String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    TextField("Название", text: $name)
                        .scaledFont(size: 17)
                        .focused($isNameFocused)
                        .submitLabel(.done)
                        .onSubmit { Task { await create() } }
                        .surfaceCard()
                    Text("Например: «Поездка в Стамбул» или «Квартира».")
                        .font(.caption)
                        .foregroundStyle(Color.inkSecondary)
                        .padding(.horizontal, 4)
                    currencyRow
                    Button {
                        Task { await create() }
                    } label: {
                        if isSaving {
                            HStack {
                                ProgressView()
                                    .tint(.white)
                                Text("Создание…")
                            }
                        } else {
                            Text("Создать")
                        }
                    }
                    .buttonStyle(.primaryPill)
                    .disabled(trimmedName.isEmpty || isSaving)
                    .padding(.top, 8)
                }
                .padding(16)
            }
            .background(Color.bg)
            .navigationTitle("Новая группа")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Отмена") { dismiss() }
                }
            }
            .onAppear { isNameFocused = true }
            .task { await loadCurrencies() }
            .errorAlert($alertMessage)
        }
        .presentationDetents([.medium, .large])
        // Введённое название нельзя потерять случайным смахиванием sheet.
        .interactiveDismissDisabled(!name.isEmpty)
    }

    /// Строка «Валюта» с меню: один экран, а не мастер — создание тусы воронка,
    /// и лишний шаг тут только теряет людей.
    ///
    /// Пока справочник не пришёл, строки нет вовсе: подставить валюту, которой
    /// человек не видел, хуже, чем завести тусу прежним умолчанием.
    @ViewBuilder
    private var currencyRow: some View {
        if let currencies, let selectedCurrency {
            Menu {
                Picker("Валюта", selection: currencyBinding) {
                    ForEach(currencies) { currency in
                        Text(verbatim: "\(currency.flag) \(currency.code) · \(currency.symbol)")
                            .tag(currency.code)
                    }
                }
            } label: {
                HStack(spacing: 8) {
                    Text("Валюта")
                        .scaledFont(size: 17)
                        .foregroundStyle(Color.ink)
                    Spacer(minLength: 8)
                    Text(verbatim: label(for: selectedCurrency, in: currencies))
                        .scaledFont(size: 17, weight: .medium)
                        .foregroundStyle(Color.inkSecondary)
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Color.inkSecondary)
                }
                .surfaceCard()
            }
            .disabled(isSaving)
            .accessibilityLabel("Валюта группы")
            .accessibilityValue(selectedCurrency)
            Text("Валюту можно поменять и потом, в настройках группы: записанные суммы смена не пересчитывает.")
                .font(.caption)
                .foregroundStyle(Color.inkSecondary)
                .padding(.horizontal, 4)
        }
    }

    private var currencyBinding: Binding<String> {
        Binding(
            get: { selectedCurrency ?? defaultCurrencyCode },
            set: { code in
                if code != selectedCurrency { didPickCurrency = true }
                selectedCurrency = code
            }
        )
    }

    private func label(for code: String, in currencies: [CurrencyInfo]) -> String {
        guard let currency = currencies.first(where: { $0.code == code }) else { return code }
        return "\(currency.flag) \(currency.code)"
    }

    /// Справочник — из общего кеша, тот же список, что в настройках группы.
    /// Ошибку не показываем: валюта здесь необязательна, и без справочника
    /// создание работает ровно как раньше.
    private func loadCurrencies() async {
        guard currencies == nil else { return }
        do {
            let result = try await session.repo.currencies { cached in apply(cached) }
            apply(result.value)
        } catch {
            // Ни кеша, ни сети — экран остаётся прежним, с одним полем.
        }
    }

    private func apply(_ list: [CurrencyInfo]) {
        guard !list.isEmpty else { return }
        currencies = list
        if selectedCurrency == nil {
            selectedCurrency = suggestedRoomCurrency(
                recent: recentCurrency,
                available: list.map(\.code)
            )
        }
    }

    private func create() async {
        guard !trimmedName.isEmpty, !isSaving else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            // Валюта уходит, только если человек её видел: иначе тусу заводит
            // прежний путь, и умолчание ставит сервер.
            let chosen = currencies == nil ? nil : selectedCurrency
            let room = try await session.api.createRoom(name: trimmedName, currency: chosen)
            Analytics.shared.track(.roomCreated(
                currency: (chosen ?? defaultCurrencyCode).lowercased(),
                picked: didPickCurrency
            ))
            // Единая инвалидация: список групп перезагрузится по dataVersion.
            session.noteDataChanged()
            Haptics.success()
            onCreated(room)
            dismiss()
        } catch {
            alertMessage = humanErrorText(error)
        }
    }
}
