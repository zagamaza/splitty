import Foundation
import Observation

/// Просьба оценить приложение в App Store.
///
/// Копит удачные моменты и решает, заслужена ли просьба; сам показ делает
/// `RootView`. Разделено намеренно: моменты случаются на листах (погашение,
/// добавление расхода), а лист в этот момент уже закрывается — системный
/// диалог с исчезающего экрана не показывается вовсе, и слот в году сгорает
/// впустую.
@Observable
final class ReviewPrompt {
    /// Общий экземпляр: моменты отмечают два разных экрана, спрашивает корень.
    static let shared = ReviewPrompt()

    /// Что именно случилось хорошего.
    enum Moment {
        /// Долг погашен — ради этой минуты приложение и ставили.
        case debtSettled
        case expenseAdded

        var weight: Int {
            switch self {
            case .debtSettled: return ReviewPrompt.requiredWeight
            case .expenseAdded: return 1
            }
        }
    }

    private static let requiredWeight = 3
    /// Пауза между просьбами. Apple режет показы до трёх в год сама, но молча:
    /// без своей отсечки мы бы считали показанным то, чего человек не видел.
    private static let quietPeriod: TimeInterval = 120 * 24 * 60 * 60

    private static let weightKey = "splitty.review.weight"
    private static let versionKey = "splitty.review.askedVersion"
    private static let dateKey = "splitty.review.askedAt"

    private static let appVersion =
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "unknown"

    private let defaults: UserDefaults

    /// true — просьба заслужена и ждёт показа. Гасится в `markAsked`.
    private(set) var isEarned = false

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func note(_ moment: Moment) {
        let weight = defaults.integer(forKey: Self.weightKey) + moment.weight
        defaults.set(weight, forKey: Self.weightKey)
        guard weight >= Self.requiredWeight, canAsk else { return }
        isEarned = true
    }

    /// Вызывается ДО показа диалога: если приложение умрёт между вызовом и
    /// показом, человек не увидит просьбу — это лучше, чем показать её дважды.
    func markAsked() {
        isEarned = false
        defaults.set(0, forKey: Self.weightKey)
        defaults.set(Self.appVersion, forKey: Self.versionKey)
        defaults.set(Date.now.timeIntervalSince1970, forKey: Self.dateKey)
    }

    private var canAsk: Bool {
        guard defaults.string(forKey: Self.versionKey) != Self.appVersion else { return false }
        let last = defaults.double(forKey: Self.dateKey)
        return last == 0 || Date.now.timeIntervalSince1970 - last >= Self.quietPeriod
    }
}
