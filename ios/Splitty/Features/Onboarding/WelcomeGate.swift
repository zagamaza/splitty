import Foundation

/// Когда показывать разовое приветствие.
///
/// Три условия, и каждое взято из живой ситуации:
/// — аккаунт его ещё не видел (ключ по номеру аккаунта, не по устройству);
/// — список групп пуст: у того, кто уже в группах, объяснять нечего, а
///   приветствие поверх работы раздражает;
/// — нет диплинка: человек пришёл по ссылке приглашения в конкретную группу,
///   и показать ему вместо неё рассказ о продукте — значит потерять переход.
func shouldShowWelcome(hasSeen: Bool, groupCount: Int, hasPendingDeeplink: Bool) -> Bool {
    guard !hasSeen else { return false }
    guard groupCount == 0 else { return false }
    return !hasPendingDeeplink
}
