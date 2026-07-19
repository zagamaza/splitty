import SwiftUI
import XCTest
@testable import Splitty

/// Рендер ключевых AI-компонентов формы расхода в PNG (`/tmp/splitty-snapshots/`)
/// через `ImageRenderer` — визуальная самопроверка вёрстки без запуска приложения.
/// Ассерты минимальные (рендер не упал, размер разумный) — смотреть надо глазами.
@MainActor
final class SnapshotRenderTests: XCTestCase {
    private let dir = URL(fileURLWithPath: "/tmp/splitty-snapshots", isDirectory: true)

    private let members = [
        User(id: 1, username: "mazanur", displayName: "Алмаз"),
        User(id: 2, username: "lyoha", displayName: "Алексей Смирнов"),
        User(id: 3, username: "sanek", displayName: "Александр Петров"),
        User(id: 4, username: "mary", displayName: "Мария Иванова"),
    ]

    /// Черновик из прототипа: поровну, веса, фикс, unknown, сбор.
    private var fixtureItems: [OperationItem] {
        [
            OperationItem(name: "Пицца Маргарита", price: 1200, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1),
                ItemShare(userId: 2, weight: 1),
            ], unknown: ["Саня"]),
            OperationItem(name: "Баурсаки", price: 500, qty: 10, shares: [
                ItemShare(userId: 1, weight: 3),
                ItemShare(userId: 2, weight: 5),
                ItemShare(userId: 4, weight: 2),
            ]),
            OperationItem(name: "Вино", price: 3000, qty: 1, shares: [
                ItemShare(userId: 1, weight: 1),
                ItemShare(userId: 2, weight: 1),
                ItemShare(userId: 4, weight: 1, amount: 500),
            ]),
            OperationItem(name: "Салат Цезарь", price: 400, qty: 1, shares: [
                ItemShare(userId: 4, weight: 1),
            ]),
            OperationItem(
                name: "Сервисный сбор", price: 510, qty: 1, shares: nil,
                kind: OperationItem.kindSurcharge,
                split: OperationItem.splitProportional, percent: 10
            ),
        ]
    }

    func testRenderReceiptInteractive() throws {
        let view = ReceiptView(
            items: fixtureItems,
            members: members,
            currency: "RUB",
            onEditItem: { _ in },
            onResolveUnknown: { _, _ in },
            onToggleSurchargeRule: { _ in }
        )
        try render(view, name: "receipt-interactive")
    }

    func testRenderReceiptReadOnly() throws {
        let view = ReceiptView(items: fixtureItems, members: members, currency: "RUB")
        try render(view, name: "receipt-readonly")
    }

    func testRenderPersonBreakdown() throws {
        let model = AddExpenseViewModel()
        // unknown убираем: разбивка показывается уже после сопоставления имён.
        model.draftItems = fixtureItems.map {
            OperationItem(
                name: $0.name, price: $0.price, qty: $0.qty, shares: $0.shares,
                kind: $0.kind, split: $0.split, percent: $0.percent, unknown: nil
            )
        }
        let shares = try XCTUnwrap(model.personShares)
        let view = PersonBreakdownCard(
            shares: shares, members: members, currency: "RUB", meId: 1
        )
        try render(view, name: "person-breakdown")
    }

    func testRenderReceiptWithPricelessItem() throws {
        // Позиция без цены — метка «цена?» и подсказка «укажите цену».
        var items = fixtureItems
        items[0] = OperationItem(name: "Пицца Маргарита", price: 0, qty: 1, shares: [
            ItemShare(userId: 1, weight: 1),
            ItemShare(userId: 2, weight: 1),
        ])
        let view = ReceiptView(
            items: items, members: members, currency: "RUB",
            onEditItem: { _ in }, onResolveUnknown: { _, _ in }, onToggleSurchargeRule: { _ in }
        )
        try render(view, name: "receipt-priceless")
    }

    func testRenderItemSheetByWeights() throws {
        // Баурсаки: веса 3/5/2 — режим «Долями», живые суммы у степперов.
        let model = sheetModel()
        try render(ItemSheetView(model: model, index: 1, meId: 1).sheetSections,
                   name: "item-sheet-weights")
    }

    func testRenderItemSheetByAmounts() throws {
        // Вино: у Марии фикс 500 — режим «Суммами», «авто» у остальных.
        let model = sheetModel()
        try render(ItemSheetView(model: model, index: 2, meId: 1).sheetSections,
                   name: "item-sheet-amounts")
    }

    /// Модель с участниками и черновиком для рендера шита позиции
    /// (участники задаются публичным selectRoom, как при выборе группы).
    private func sheetModel() -> AddExpenseViewModel {
        let model = AddExpenseViewModel()
        model.selectRoom(RoomSummary(
            id: "r1", name: "Тест", createdAt: Date(timeIntervalSince1970: 0),
            isArchived: false, members: members, memberCount: members.count,
            currency: "RUB", totalSpent: 0, myBalance: 0
        ))
        model.draftItems = fixtureItems
        return model
    }

    func testRenderRecordingOverlay() throws {
        // Длинный транскрипт: окно-«телесуфлёр» держит хвост, верх тает в маске.
        let transcript = "Ужинали в Лакки. Пицца тысяча двести — я, Лёха и Саня. "
            + "Баурсаков на пятьсот взяли десять: Лёха пять, я три, Маша два. "
            + "Вино три тысячи — с Маши пятьсот, остальное на нас с Лёхой"
        try render(
            RecordingOverlay(transcript: transcript, isCancelling: false,
                             isLocked: false, drag: .zero),
            name: "recording-overlay", height: 780
        )
    }

    func testRenderRecordingOverlayWithHints() throws {
        try render(
            RecordingOverlay(
                transcript: "Пицца стоила шестьсот",
                isCancelling: false, isLocked: false, drag: .zero,
                hints: ["Кто такой «Саня»?", "Сколько стоит «Салат»?", "Кто платил?"]
            ),
            name: "recording-overlay-hints", height: 780
        )
    }

    func testRenderRecordingOverlayCancelling() throws {
        try render(
            RecordingOverlay(transcript: "Пицца шестьсот…", isCancelling: true,
                             isLocked: false, drag: CGSize(width: -90, height: 0)),
            name: "recording-overlay-cancel", height: 780
        )
    }

    func testRenderRecordingOverlayLocked() throws {
        // Запись закреплена свайпом вверх: кнопки «Отмена»/«Готово».
        try render(
            RecordingOverlay(transcript: "Баурсаки на пятьсот, Лёха пять…",
                             isCancelling: false, isLocked: true, drag: .zero,
                             startedAt: Date(timeIntervalSinceNow: -47)),
            name: "recording-overlay-locked", height: 780
        )
    }

    // MARK: render

    /// Рендерит вью на подложке экрана (ширина iPhone, фон `Color.bg`) в 2x PNG.
    /// `height` — для экранов со скроллом (NavigationStack/ScrollView не имеют
    /// собственной высоты в ImageRenderer).
    private func render<V: View>(
        _ view: V, name: String, width: CGFloat = 390, height: CGFloat? = nil
    ) throws {
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let content = view
            .padding(height == nil ? 20 : 0)
            .frame(width: width, height: height)
            .background(Color.bg)
            // UserAvatarView читает SessionStore из environment (кеш аватарок).
            .environment(SessionStore())
        let renderer = ImageRenderer(content: content)
        renderer.scale = 2
        renderer.proposedSize = ProposedViewSize(width: width, height: height)
        let image = try XCTUnwrap(renderer.uiImage, "рендер не удался: \(name)")
        XCTAssertGreaterThan(image.size.height, 60, "подозрительно низкий рендер: \(name)")
        let data = try XCTUnwrap(image.pngData())
        try data.write(to: dir.appendingPathComponent("\(name).png"))
    }
}
