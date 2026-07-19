import SwiftUI
import UIKit

/// Прозрачная UIKit-поверхность для hold-to-talk микрофона.
///
/// Зачем не SwiftUI-жест: `DragGesture` даже с `minimumDistance: 0` проходит
/// арбитраж распознавания жестов, и первый `onChanged` приходит с опозданием
/// ~100–300 мс (на коротком тапе события вообще отдаются пачкой на
/// отпускании — «микроанимация после release»). `beginTracking` у `UIControl`
/// вызывается прямо из `touchesBegan` в том же кадре, без арбитража — это
/// самый низколатентный путь получить касание (ниже, чем
/// `UILongPressGestureRecognizer`, который в арбитраже участвует).
///
/// Палец может уходить за границы контрола (свайп вверх — замок, влево —
/// отмена): UIKit продолжает слать tracking-события контролу, начавшему
/// касание.
struct MicTouchSurface: UIViewRepresentable {
    /// Снимок касания для хендлеров.
    struct Sample {
        /// `UITouch.timestamp` (system uptime) — для замера латентности
        /// доставки: `ProcessInfo.processInfo.systemUptime - timestamp`.
        let timestamp: TimeInterval
        /// Смещение от точки начала касания (координаты окна).
        let translation: CGSize
    }

    var isEnabled: Bool = true
    var onBegan: (Sample) -> Void
    var onMoved: (Sample) -> Void
    var onEnded: (Sample) -> Void
    /// Система отобрала касание (входящий звонок, свернули приложение,
    /// чужой распознаватель) — запись отменяется, не отправляется.
    var onCancelled: () -> Void

    func makeUIView(context: Context) -> TouchControl {
        TouchControl()
    }

    func updateUIView(_ control: TouchControl, context: Context) {
        control.isEnabled = isEnabled
        control.onBegan = onBegan
        control.onMoved = onMoved
        control.onEnded = onEnded
        control.onCancelled = onCancelled
    }

    final class TouchControl: UIControl {
        var onBegan: ((Sample) -> Void)?
        var onMoved: ((Sample) -> Void)?
        var onEnded: ((Sample) -> Void)?
        var onCancelled: (() -> Void)?
        /// Точка начала касания в координатах окна — база для translation.
        private var startPoint: CGPoint = .zero

        override func beginTracking(_ touch: UITouch, with event: UIEvent?) -> Bool {
            startPoint = touch.location(in: nil)
            onBegan?(sample(for: touch))
            return true
        }

        override func continueTracking(_ touch: UITouch, with event: UIEvent?) -> Bool {
            onMoved?(sample(for: touch))
            return true
        }

        override func endTracking(_ touch: UITouch?, with event: UIEvent?) {
            super.endTracking(touch, with: event)
            if let touch {
                onEnded?(sample(for: touch))
            } else {
                onCancelled?()
            }
        }

        override func cancelTracking(with event: UIEvent?) {
            super.cancelTracking(with: event)
            onCancelled?()
        }

        private func sample(for touch: UITouch) -> Sample {
            let p = touch.location(in: nil)
            return Sample(
                timestamp: touch.timestamp,
                translation: CGSize(width: p.x - startPoint.x, height: p.y - startPoint.y)
            )
        }
    }
}
