import XCTest
@testable import Splitty

/// Голосовой ввод падал с системным английским «Session activation failed»:
/// ошибку AVAudioSession никто не заворачивал, а humanErrorText для незнакомой
/// ошибки отдаёт localizedDescription как есть. Человек видел строку, из
/// которой не следует ни причина, ни что делать.
final class AudioSessionErrorTests: XCTestCase {
    func testSessionBusyHasHumanText() {
        let text = humanErrorText(AudioRecorderError.sessionBusy)

        XCTAssertFalse(text.isEmpty)
        XCTAssertFalse(text.contains("Session activation failed"),
                       "наружу ушёл системный текст вместо нашего")
        XCTAssertTrue(text.contains("Микрофон"),
                      "текст должен называть причину — занятый микрофон, got: \(text)")
    }

    /// Соседние ошибки записи не должны схлопнуться в один текст: «занят
    /// микрофон» и «нет доступа к микрофону» чинятся по-разному.
    func testRecorderErrorsAreDistinct() {
        let busy = humanErrorText(AudioRecorderError.sessionBusy)
        let failed = humanErrorText(AudioRecorderError.failedToStart)
        let starting = humanErrorText(AudioRecorderError.alreadyStarting)

        XCTAssertNotEqual(busy, failed)
        XCTAssertNotEqual(busy, starting)
        XCTAssertTrue(failed.contains("доступ"), "got: \(failed)")
    }
}
