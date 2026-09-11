import Foundation
import Network
import Observation

/// Монитор доступности сети (NWPathMonitor).
/// `isOnline` обновляется на главном потоке; экраны наблюдают его через
/// Observation (`session.isOnline`) — глобальный офлайн-баннер и офлайн-ветки
/// (outbox расходов, запрет редактирования синхронизированных операций).
@Observable
final class NetworkMonitor {
    /// true — есть путь в сеть. Стартовое значение true: до первого апдейта
    /// монитора не мигаем офлайн-баннером.
    private(set) var isOnline = true

    private let monitor = NWPathMonitor()
    private var isStarted = false

    /// Начать слежение.
    ///
    /// Отдельно от `init`, и это не про стиль. `NWPathMonitor` смотрит ВСЕ
    /// интерфейсы, включая локальные, и система считает это обращением к
    /// локальной сети — со своим системным запросом. Пока монитор стартовал в
    /// конструкторе сессии, запрос выскакивал на первом кадре: человек ещё не
    /// видел ни одного экрана, а у него уже спрашивают про домашнюю сеть.
    ///
    /// Спрашивать там не за что: офлайн-баннер и офлайн-ветки живут только под
    /// входом — у гостя нет ни кеша, ни очереди расходов. Поэтому слежение
    /// начинается вместе с сессией, а до неё `isOnline` отдаёт `true`: не зная
    /// ничего, мигать баннером хуже, чем молчать.
    func start() {
        guard !isStarted else { return }
        isStarted = true
        monitor.pathUpdateHandler = { [weak self] path in
            let online = path.status == .satisfied
            Task { @MainActor [weak self] in
                guard let self, self.isOnline != online else { return }
                self.isOnline = online
            }
        }
        monitor.start(queue: DispatchQueue(label: "splitty.network-monitor"))
    }

    deinit {
        if isStarted { monitor.cancel() }
    }
}
