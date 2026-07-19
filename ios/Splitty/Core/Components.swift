import SwiftUI

// MARK: - Глоссарий состояний баланса

/// Единый словарь формулировок про долги: раньше «расчёт» / «в расчёте» /
/// «Вы рассчитались» / «Все долги погашены» жили вразнобой по экранам.
/// Одна точка правды — как `MoneyText` для цвета денег.
enum Glossary {
    /// Нулевой баланс — короткая подпись строки/карточки.
    static let settled = "в расчёте"
    /// Нулевой баланс — заголовок/hero-состояние.
    static let settledHero = "Все долги погашены"

    /// Долги комнаты неисчислимы (легаси-данные бота, доли не сходятся). Сервер
    /// шлёт debtsUnavailable вместе с myBalance=0, поэтому без отдельной подписи
    /// экран сказал бы «в расчёте» — ложное утверждение о деньгах.
    /// Тексты — паритет с android strings.xml/group_debts_unavailable_*.
    static let debtsUnavailableShort = "долги не считаются"
    static let debtsUnavailableHero = "Долги не считаются"
    static let debtsUnavailableSubtitle =
        "Данные группы из старой версии — балансы не сходятся. Операции и итоги доступны."

    /// Подпись направления долга для суммы: положительная — должны вам.
    /// Нулевая ветка обязательна: тернарник «>0 ? вам : вы» при нуле врал.
    static func balanceCaption(_ sum: Int) -> String {
        if sum > 0 { return "вам должны" }
        if sum < 0 { return "вы должны" }
        return settled
    }
}

// MARK: - Русская плюрализация

/// Форма слова по числу: pluralRu(1, "операция", "операции", "операций").
func pluralRu(_ n: Int, _ one: String, _ few: String, _ many: String) -> String {
    let mod100 = abs(n) % 100
    if (11...14).contains(mod100) { return many }
    switch mod100 % 10 {
    case 1: return one
    case 2...4: return few
    default: return many
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
    func errorAlert(_ message: Binding<String?>, title: String = "Ошибка") -> some View {
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
    if let urlError = error as? URLError {
        switch urlError.code {
        case .notConnectedToInternet, .networkConnectionLost, .dataNotAllowed:
            return "Нет соединения с интернетом. Проверьте сеть и попробуйте ещё раз"
        case .timedOut:
            return "Сервер долго не отвечает. Попробуйте ещё раз"
        default:
            return "Не получилось связаться с сервером. Попробуйте ещё раз"
        }
    }
    return error.localizedDescription
}
