import SwiftUI

// MARK: - Глоссарий состояний баланса

/// Единый словарь формулировок про долги: раньше «расчёт» / «в расчёте» /
/// «Вы рассчитались» / «Все долги погашены» жили вразнобой по экранам.
/// Одна точка правды — как `MoneyText` для цвета денег.
enum Glossary {
    /// Нулевой баланс — короткая подпись строки/карточки.
    static var settled: String { String(localized: "в расчёте") }
    /// Нулевой баланс — заголовок/hero-состояние.
    static var settledHero: String { String(localized: "Все долги погашены") }

    /// Долги комнаты неисчислимы (легаси-данные бота, доли не сходятся). Сервер
    /// шлёт debtsUnavailable вместе с myBalance=0, поэтому без отдельной подписи
    /// экран сказал бы «в расчёте» — ложное утверждение о деньгах.
    /// Тексты — паритет с android strings.xml/group_debts_unavailable_*.
    static var debtsUnavailableShort: String { String(localized: "долги не считаются") }
    static var debtsUnavailableHero: String { String(localized: "Долги не считаются") }
    static var debtsUnavailableSubtitle: String {
        String(localized: "Данные группы из старой версии — балансы не сходятся. Операции и итоги доступны.")
    }

    /// Подпись направления долга для суммы: положительная — должны вам.
    /// Нулевая ветка обязательна: тернарник «>0 ? вам : вы» при нуле врал.
    static func balanceCaption(_ sum: Int) -> String {
        if sum > 0 { return String(localized: "вам должны") }
        if sum < 0 { return String(localized: "вы должны") }
        return settled
    }
}

// MARK: - Единое состояние «не удалось загрузить»

/// Failed-state с кнопкой «Повторить» — один вид на всех экранах
/// (раньше было три разных стиля той же кнопки).
struct FailedStateView: View {
    let message: String
    let retry: () async -> Void

    var body: some View {
        ContentUnavailableView {
            Label("Не удалось загрузить", systemImage: "wifi.exclamationmark")
        } description: {
            Text(message)
        } actions: {
            Button("Повторить") {
                Task { await retry() }
            }
            .buttonStyle(.borderedProminent)
            .tint(Color.accent)
        }
    }
}

// MARK: - Единый alert ошибки

extension View {
    /// Alert «Ошибка» поверх optional-сообщения: одна точка вместо
    /// скопированного Binding-бойлерплейта в каждом экране.
    func errorAlert(_ message: Binding<String?>, title: LocalizedStringKey = "Ошибка") -> some View {
        alert(
            title,
            isPresented: Binding(
                get: { message.wrappedValue != nil },
                set: { if !$0 { message.wrappedValue = nil } }
            )
        ) {
            Button("ОК", role: .cancel) {}
        } message: {
            Text(message.wrappedValue ?? "")
        }
    }
}

// MARK: - Человеческие тексты ошибок

/// Переводит сетевой/системный сбой в понятный пользователю текст:
/// сырые `localizedDescription` URLSession в алертах пугают жаргоном.
func humanErrorText(_ error: Error) -> String {
    // APIClient заворачивает ЛЮБУЮ сетевую ошибку в APIError.transport(URLError),
    // поэтому прямая проверка `as? URLError` не срабатывала никогда: вместо
    // «нет интернета» / «сервер долго не отвечает» пользователь всегда видел
    // общее «Нет соединения с сервером».
    if let apiError = error as? APIError, case .transport(let underlying) = apiError {
        return humanErrorText(underlying)
    }
    if let urlError = error as? URLError {
        switch urlError.code {
        case .notConnectedToInternet, .networkConnectionLost, .dataNotAllowed:
            return String(localized: "Нет соединения с интернетом. Проверьте сеть и попробуйте ещё раз")
        case .timedOut:
            return String(localized: "Сервер долго не отвечает. Попробуйте ещё раз")
        default:
            return String(localized: "Не получилось связаться с сервером. Попробуйте ещё раз")
        }
    }
    return error.localizedDescription
}

// MARK: - Имя участника

/// Имя со сноской «(вы)» для себя. Отдельная функция, а не тернарник по месту:
/// в другом языке сноска может стоять иначе, и порядок задаёт перевод.
func memberLabel(_ name: String, isMe: Bool) -> String {
    isMe ? String(localized: "\(name) (вы)") : name
}
