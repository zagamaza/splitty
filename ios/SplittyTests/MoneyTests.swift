import SwiftUI
import XCTest
@testable import Splitty

final class MoneyTests: XCTestCase {
    // MARK: rubles(_:) — формат «1 200 ₽», пробел — разделитель тысяч

    func testRublesZero() {
        XCTAssertEqual(rubles(0), "0 ₽")
    }

    func testRublesSingleDigit() {
        XCTAssertEqual(rubles(5), "5 ₽")
    }

    func testRublesThousand() {
        XCTAssertEqual(rubles(1000), "1 000 ₽")
    }

    func testRublesMillions() {
        XCTAssertEqual(rubles(1234567), "1 234 567 ₽")
    }

    // MARK: money(_:currency:) — формат единый с рублями: «1 234 $»

    func testMoneyRub() {
        XCTAssertEqual(money(1200, currency: "RUB"), "1 200 ₽")
    }

    func testMoneyUsd() {
        XCTAssertEqual(money(1234, currency: "USD"), "1 234 $")
    }

    func testMoneyEur() {
        XCTAssertEqual(money(5, currency: "EUR"), "5 €")
    }

    func testMoneyIdr() {
        XCTAssertEqual(money(1500000, currency: "IDR"), "1 500 000 Rp")
    }

    func testMoneyUnknownCurrencyShowsCode() {
        // Незнакомый код — сам код вместо символа.
        XCTAssertEqual(money(700, currency: "GBP"), "700 GBP")
    }

    func testMoneyKztUzs() {
        XCTAssertEqual(money(700, currency: "KZT"), "700 ₸")
        XCTAssertEqual(money(12000, currency: "UZS"), "12 000 сум")
    }

    func testMoneyNegative() {
        XCTAssertEqual(money(-4300, currency: "USD"), "-4 300 $")
    }

    func testMoneyZero() {
        XCTAssertEqual(money(0, currency: "EUR"), "0 €")
    }

    func testMoneyRangeInCurrency() {
        XCTAssertEqual(moneyRange(333, 334, currency: "USD"), "333–334 $")
    }

    func testCurrencySymbol() {
        XCTAssertEqual(currencySymbol("RUB"), "₽")
        XCTAssertEqual(currencySymbol("USD"), "$")
        XCTAssertEqual(currencySymbol("EUR"), "€")
        XCTAssertEqual(currencySymbol("IDR"), "Rp")
        XCTAssertEqual(currencySymbol("KZT"), "₸")
        XCTAssertEqual(currencySymbol("UZS"), "сум")
        XCTAssertEqual(currencySymbol("GBP"), "GBP")
    }

    // MARK: aggregateByCurrency — суммы в разных валютах не складываются

    func testAggregateSumsPerCurrencyAndSortsByMagnitude() {
        let totals = aggregateByCurrency([
            CurrencySum(currency: "RUB", sum: 500),
            CurrencySum(currency: "USD", sum: -1200),
            CurrencySum(currency: "RUB", sum: 300),
        ])
        // USD |−1200| > RUB |800|: основная валюта — с наибольшим модулем.
        XCTAssertEqual(totals, [
            CurrencySum(currency: "USD", sum: -1200),
            CurrencySum(currency: "RUB", sum: 800),
        ])
    }

    func testAggregateDropsZeroTotals() {
        let totals = aggregateByCurrency([
            CurrencySum(currency: "RUB", sum: 500),
            CurrencySum(currency: "RUB", sum: -500),
            CurrencySum(currency: "EUR", sum: 10),
        ])
        XCTAssertEqual(totals, [CurrencySum(currency: "EUR", sum: 10)])
    }

    func testAggregateEmptyInputIsEmpty() {
        XCTAssertEqual(aggregateByCurrency([]), [])
    }

    func testAggregateTieBreaksByCurrencyCode() {
        // Равные |суммы| — стабильный порядок по коду валюты.
        let totals = aggregateByCurrency([
            CurrencySum(currency: "USD", sum: 100),
            CurrencySum(currency: "EUR", sum: -100),
        ])
        XCTAssertEqual(totals, [
            CurrencySum(currency: "EUR", sum: -100),
            CurrencySum(currency: "USD", sum: 100),
        ])
    }

    func testFriendBalanceTotalsUsesAggregation() {
        // FriendBalance.totals — агрегация повалютно, нули скрыты.
        let friend = FriendBalance(
            user: User(id: 1, username: nil, displayName: "Алмаз"),
            totalsByCurrency: [
                CurrencySum(currency: "RUB", sum: 0),
                CurrencySum(currency: "USD", sum: -70),
            ],
            rooms: []
        )
        XCTAssertEqual(friend.totals, [CurrencySum(currency: "USD", sum: -70)])
    }

    // Цветовое правило денег живёт в MoneyText.Role (DesignSystem.swift) —
    // проверяется через роль .auto в SharesTests/позициях, а не отдельной функцией.

    /// Swift усекает деление к нулю: у отрицательной суммы остаток отрицательный,
    /// и наивное `$0 < remainder` не срабатывало — Σ долей расходилась с sum
    /// вопреки инварианту в доккомментарии `shares`.
    func testSharesConservesNegativeSums() {
        for sum in [0, -1, -5, -10, -100, -999] {
            for count in 1...7 {
                XCTAssertEqual(shares(sum: sum, count: count).reduce(0, +), sum,
                               "shares(sum: \(sum), count: \(count))")
            }
        }
    }

    func testMoneyRangeKeepsSignOfLowerBound() {
        XCTAssertEqual(moneyRange(-100, 50, currency: "RUB"), "-100–50 ₽")
    }
}
