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

    init() {
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
        monitor.cancel()
    }
}
