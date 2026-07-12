import SwiftUI
import XCTest
@testable import Splitty

/// Логика дашборда «Итоги» v2: назначение категориальных цветов участникам
/// (стабильность по user.id, >6 → «прочие»), нетто-балансы (net = paid − share),
/// агрегация трат по дням недели, сегменты доната и подписи месяцев.
final class GroupTotalsLogicTests: XCTestCase {
    private func user(_ id: Int, _ name: String = "Юзер") -> User {
        User(id: id, username: nil, displayName: name)
    }

    // MARK: Палитра участников

    func testPaletteSizeMatchesTheme() {
        // Правило «первые 6 по id» обязано совпадать с размером палитры токенов.
        XCTAssertEqual(Color.chartCategorical.count, MemberPalette.colorCount)
    }

    func testColorIndicesAssignedByUserIdAscending() {
        // Порядок входа не важен: индексы — по возрастанию id.
        let map = MemberPalette.colorIndices(memberIds: [30, 10, 20])
        XCTAssertEqual(map, [10: 0, 20: 1, 30: 2])

        // Стабильность: тот же состав → та же карта (дубликаты схлопываются).
        let again = MemberPalette.colorIndices(memberIds: [20, 30, 10, 10])
        XCTAssertEqual(again, map)
    }

    func testColorIndicesNeverCyclePastSix() {
        // 8 участников: цвета получают только первые 6 по id, 7-й и 8-й —
        // без записи (рисуются inkSecondary), палитра не циклится.
        let ids = [8, 3, 5, 1, 7, 2, 6, 4]
        let map = MemberPalette.colorIndices(memberIds: ids)
        XCTAssertEqual(map.count, 6)
        XCTAssertEqual(map[1], 0)
        XCTAssertEqual(map[6], 5)
        XCTAssertNil(map[7])
        XCTAssertNil(map[8])
    }

    // MARK: Нетто-балансы (net = paid − share)

    func testNetBalancesComputedAndSortedDescending() {
        let paid = [
            MemberSum(user: user(10, "Загир"), sum: 3000),
            MemberSum(user: user(20, "Алмаз"), sum: 1200),
        ]
        let share = [
            MemberSum(user: user(10, "Загир"), sum: 2100),
            MemberSum(user: user(20, "Алмаз"), sum: 2100),
        ]
        let nets = DashboardMath.netBalances(paid: paid, share: share)
        XCTAssertEqual(nets.map(\.user.id), [10, 20])
        XCTAssertEqual(nets.map(\.net), [900, -900])
    }

    func testNetBalancesIncludeMembersFromEitherListAndZeroNet() {
        // 30 есть только в share (ничего не платил), 40 — только в paid;
        // 10 в нуле — тоже присутствует. Равные net упорядочены по id.
        let paid = [
            MemberSum(user: user(10), sum: 500),
            MemberSum(user: user(40), sum: 300),
        ]
        let share = [
            MemberSum(user: user(10), sum: 500),
            MemberSum(user: user(30), sum: 300),
        ]
        let nets = DashboardMath.netBalances(paid: paid, share: share)
        XCTAssertEqual(nets.map(\.user.id), [40, 10, 30])
        XCTAssertEqual(nets.map(\.net), [300, 0, -300])
    }

    // MARK: Агрегация по дням недели

    func testWeekdayTotalsAggregateOntoMondayFirstWeek() {
        // 2026-06-29 — понедельник, 2026-07-06 — следующий понедельник,
        // 2026-07-05 — воскресенье (суммы понедельников складываются).
        let byDay = [
            DailySum(date: "2026-06-29", sum: 100),
            DailySum(date: "2026-07-06", sum: 50),
            DailySum(date: "2026-07-05", sum: 300),
            DailySum(date: "битая строка", sum: 999), // пропускается
        ]
        let totals = DashboardMath.weekdayTotals(byDay: byDay)
        XCTAssertEqual(totals.count, 7)
        XCTAssertEqual(totals[0], 150) // пн
        XCTAssertEqual(totals[6], 300) // вс
        XCTAssertEqual(totals[1...5].reduce(0, +), 0)
    }

    // MARK: Донат «Кто платил»

    func testDonutSlicesKeepAllMembersUpToSix() {
        let paid = (1...6).map { MemberSum(user: user($0, "У\($0)"), sum: $0 * 100) }
            + [MemberSum(user: user(9, "Ноль"), sum: 0)] // нулевые убираются
        let slices = DashboardMath.donutSlices(paid: paid)
        XCTAssertEqual(slices.count, 6)
        XCTAssertEqual(slices.map(\.sum), [600, 500, 400, 300, 200, 100])
        XCTAssertTrue(slices.allSatisfy { $0.userId != nil })
    }

    func testDonutSlicesFoldSeventhAndBeyondIntoOthers() throws {
        // 8 плательщиков → топ-5 сегментов + «Прочие» с суммой остальных.
        let paid = (1...8).map { MemberSum(user: user($0, "У\($0)"), sum: $0 * 100) }
        let slices = DashboardMath.donutSlices(paid: paid)
        XCTAssertEqual(slices.count, 6)
        XCTAssertEqual(slices.prefix(5).map(\.userId), [8, 7, 6, 5, 4])
        let others = try XCTUnwrap(slices.last)
        XCTAssertNil(others.userId)
        XCTAssertEqual(others.label, "Прочие")
        XCTAssertEqual(others.sum, 300 + 200 + 100)
    }
}
