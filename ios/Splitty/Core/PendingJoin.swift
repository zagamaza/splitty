import Foundation
import Observation

/// Отложенное намерение вступить в группу: код из диплинка, который ещё не
/// исполнен, потому что пользователь не авторизован (или приложение только
/// стартует и корневой экран ещё не появился).
///
/// Переживает перезапуск приложения (UserDefaults) намеренно: путь «тап по
/// ссылке → экран входа → вход через Google/Apple» уводит в другое приложение
/// или в веб-лист, и на слабом устройстве нас за это время выгружают. Потерять
/// намерение здесь — значит показать человеку, пришедшему по приглашению,
/// пустой список групп без единого объяснения.
///
/// Хранится код комнаты — публичный идентификатор из ссылки, которую и так
/// переслали в мессенджере, — и `ownerUserId`: тот, кто был в аккаунте, когда
/// ссылка пришла. Владельца хранить обязательно, и хранить именно рядом с
/// намерением: намерение переживает ПРОТУХШУЮ сессию (`expireSession`) и смерть
/// процесса, а любой признак «кто тут был до этого» в памяти к моменту входа
/// уже потерян — путь «ссылка → экран входа → Google/Apple» уводит из
/// приложения. Порт Android `PendingJoinStore`.
@Observable
final class PendingJoin {
    /// Общий экземпляр: намерение ставит `SplittyApp` (обработчик ссылки),
    /// исполняет `RootView`, а чистит `SessionStore.logout` — передавать его
    /// через environment пришлось бы через всю иерархию ради трёх точек.
    static let shared = PendingJoin()

    private static let storageKey = "splitty.pendingJoinRoomId"
    private static let ownerKey = "splitty.pendingJoinOwnerId"

    private let defaults: UserDefaults

    /// Код комнаты, ожидающий вступления; nil — ждать нечего.
    private(set) var roomId: String?

    /// Владелец намерения — id пользователя, вошедшего в момент прихода ссылки.
    /// nil — ссылку открыл гость, и намерение достанется первому же вошедшему:
    /// это и есть штатный путь приглашения.
    private(set) var ownerUserId: Int?

    /// `defaults` подменяется в тестах, чтобы прогон не оставлял намерение
    /// в реальном хранилище приложения.
    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        roomId = defaults.string(forKey: Self.storageKey)
        ownerUserId = defaults.object(forKey: Self.ownerKey) as? Int
    }

    /// Запомнить намерение (пришла ссылка). `ownerId` — id вошедшего ПРЯМО
    /// СЕЙЧАС; nil, если ссылку открыл гость.
    func set(_ roomId: String, ownerId: Int? = nil) {
        self.roomId = roomId
        ownerUserId = ownerId
        defaults.set(roomId, forKey: Self.storageKey)
        if let ownerId {
            defaults.set(ownerId, forKey: Self.ownerKey)
        } else {
            defaults.removeObject(forKey: Self.ownerKey)
        }
    }

    /// Свести намерение с вошедшим пользователем (зовётся из
    /// `SessionStore.adoptOwner` на каждом входе).
    ///
    /// Владельца ещё нет — намерение достаётся `userId`: ссылку открыл гость,
    /// ровно так работает приглашение. Владелец ДРУГОЙ — намерение
    /// выбрасывается: сессия предыдущего человека могла протухнуть (её сброс
    /// приглашение намеренно СОХРАНЯЕТ — тот же человек переавторизуется и
    /// дойдёт до группы), а вошёл на устройстве уже кто-то другой, и без этой
    /// проверки он молча вступил бы в чужую приватную группу.
    ///
    /// Раньше здесь стояла безусловная чистка «сменился владелец → забыть»,
    /// и она убивала приглашение самого гостя: `expireSession` оставляет
    /// `ownerUserId` прошлого аккаунта, поэтому «A разлогинило по 401 → гость B
    /// открыл ссылку → B вошёл» выглядело сменой владельца.
    func reconcileOwner(_ userId: Int) {
        guard roomId != nil else { return }
        switch ownerUserId {
        case userId: break // намерение уже наше
        case nil: setOwner(userId)
        default: clear()
        }
    }

    /// Забыть намерение. Обязателен в `logout`: иначе следующий человек,
    /// вошедший на этом устройстве, молча вступил бы в чужую группу.
    ///
    /// Зовётся по РЕЗУЛЬТАТУ вступления, а не перед запросом: см.
    /// `RootView.joinPendingRoom` — временный сбой не должен стоить
    /// приглашения.
    func clear() {
        roomId = nil
        ownerUserId = nil
        defaults.removeObject(forKey: Self.storageKey)
        defaults.removeObject(forKey: Self.ownerKey)
    }

    private func setOwner(_ userId: Int) {
        ownerUserId = userId
        defaults.set(userId, forKey: Self.ownerKey)
    }
}

// MARK: - Исход попытки вступления

extension APIError {
    /// true — вступление по этой ссылке не получится НИКОГДА: комнаты нет
    /// (404) или доступ к ней закрыт (403). Повторять такое бессмысленно,
    /// поэтому отложенное намерение после такого ответа стирается —
    /// иначе один и тот же алерт встречал бы человека при каждом запуске.
    ///
    /// Всё остальное (нет сети, таймаут, 5xx) считается временным: намерение
    /// переживает сбой и исполнится при следующей попытке.
    var isTerminalJoinFailure: Bool {
        guard case .server(let status, let code, _) = self else { return false }
        return status == 404 || status == 403 || code == "not_found" || code == "forbidden"
    }
}

// MARK: - Тексты ошибок вступления по ссылке

/// Человеческий текст ошибки вступления по приглашению.
///
/// Пользователь диплинка не нажимал кнопку «Присоединиться» и не вводил код —
/// для него это «открыл ссылку, и что-то пошло не так». Сырое «Не найдено»
/// от сервера в этом контексте не объясняет ничего: нужно сказать, что именно
/// не так со ссылкой и что с этим делать.
func joinLinkErrorText(_ error: Error) -> String {
    if let apiError = error as? APIError, case .server(let status, let code, _) = apiError {
        if status == 404 || code == "not_found" {
            return "Группа не найдена. Возможно, её удалили или ссылка-приглашение устарела"
        }
        if status == 403 || code == "forbidden" {
            return "Нет доступа к этой группе. Попросите участника прислать новое приглашение"
        }
    }
    return humanErrorText(error)
}
