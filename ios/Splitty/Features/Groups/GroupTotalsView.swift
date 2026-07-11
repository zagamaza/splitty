import Charts
import SwiftUI

/// «Итоги» — дашборд группы v2 (sheet по чипу «Итоги», лейбл не менять):
/// плитки 2×2 (всего/за месяц/операций/средний чек), «Динамика по месяцам»,
/// «Траты по дням» (30 дней), донат «Кто платил», бары «Чья доля»,
/// diverging «Баланс участников», «По дням недели», топ-5 расходов.
/// Все суммы — в валюте комнаты (`statistics.currency`); участники окрашены
/// фиксированной палитрой `Color.chartCategorical` (по user.id ASC, см.
/// `MemberPalette`); magnitude-графики — единым `Color.chartAccent`;
/// текст — только ink/inkSecondary (правила дата-виза).
struct GroupTotalsView: View {
    let roomId: String

    @Environment(SessionStore.self) private var session
    @Environment(\.dismiss) private var dismiss
    @State private var stats: Statistics?
    @State private var errorMessage: String?

    init(roomId: String) {
        self.roomId = roomId
    }

    var body: some View {
        NavigationStack {
            content
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.bg)
                .navigationTitle("Итоги")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Готово") { dismiss() }
                    }
                }
                .task { await load() }
        }
        .presentationDetents([.large])
    }

    @ViewBuilder
    private var content: some View {
        if let stats {
            if stats.totalSpent == 0 && stats.topOperations.isEmpty {
                emptyState
            } else {
                dashboard(stats)
            }
        } else if let errorMessage {
            ContentUnavailableView {
                Label("Не удалось загрузить", systemImage: "wifi.exclamationmark")
            } description: {
                Text(errorMessage)
            } actions: {
                Button("Повторить") {
                    Task { await load() }
                }
            }
        } else {
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    /// Пустая комната: дружелюбное состояние вместо нулевых графиков.
    private var emptyState: some View {
        ContentUnavailableView {
            Label("Пока нет расходов", systemImage: "chart.bar")
        } description: {
            Text("Добавьте первый расход — здесь появится статистика группы")
        }
    }

    private func dashboard(_ stats: Statistics) -> some View {
        // Один человек — один цвет во всех графиках: карта id → индекс палитры
        // строится по объединению участников обоих списков статистики.
        let palette = MemberPalette.colorIndices(
            memberIds: (stats.paidByMember + stats.shareByMember).map(\.user.id)
        )
        let slices = DashboardMath.donutSlices(paid: stats.paidByMember)
        let nets = DashboardMath.netBalances(paid: stats.paidByMember, share: stats.shareByMember)
        let weekdays = DashboardMath.weekdayTotals(byDay: stats.byDay)
        return ScrollView {
            VStack(spacing: 16) {
                statTiles(stats)
                if !stats.byMonth.isEmpty {
                    MonthlySpendingCard(byMonth: stats.byMonth, currency: stats.currency)
                }
                DailySpendingCard(byDay: stats.byDay, currency: stats.currency)
                if !slices.isEmpty {
                    PaidDonutCard(
                        slices: slices,
                        palette: palette,
                        totalSpent: stats.totalSpent,
                        currency: stats.currency
                    )
                }
                if let shares = MemberBarsCard.prepared(stats.shareByMember) {
                    MemberBarsCard(
                        title: "Чья доля",
                        bars: shares,
                        currency: stats.currency,
                        palette: palette
                    )
                }
                if !nets.isEmpty {
                    MemberNetCard(nets: nets, currency: stats.currency)
                }
                if weekdays.contains(where: { $0 > 0 }) {
                    WeekdayCard(totals: weekdays)
                }
                if !stats.topOperations.isEmpty {
                    topOperationsCard(stats)
                }
            }
            .padding(16)
        }
    }

    // MARK: Стат-плитки 2×2

    /// Плитки: «Всего потрачено», «За <месяц>», «Операций», «Средний чек».
    /// Иконки тонированы своим цветом палитры (декоративно, identity не несут).
    private func statTiles(_ stats: Statistics) -> some View {
        let average = stats.operationCount > 0 ? stats.totalSpent / stats.operationCount : 0
        return VStack(spacing: 16) {
            HStack(spacing: 16) {
                statTile(title: "Всего потрачено", icon: "banknote", tint: 0) {
                    MoneyText(stats.totalSpent, role: .neutral, size: 22, currency: stats.currency)
                }
                statTile(title: "За \(Self.currentMonthName())", icon: "calendar", tint: 1) {
                    MoneyText(stats.monthSpent, role: .neutral, size: 22, currency: stats.currency)
                }
            }
            HStack(spacing: 16) {
                statTile(title: "Операций", icon: "list.bullet", tint: 2) {
                    Text("\(stats.operationCount)")
                        .font(.system(size: 22, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(Color.ink)
                }
                statTile(title: "Средний чек", icon: "chart.bar", tint: 3) {
                    MoneyText(average, role: .neutral, size: 22, currency: stats.currency)
                }
            }
        }
    }

    private func statTile(
        title: String,
        icon: String,
        tint: Int,
        @ViewBuilder value: () -> some View
    ) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Image(systemName: icon)
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Color.chartCategorical[tint])
                    .frame(width: 18, height: 18)
                Text(title)
                    .sectionHeaderStyle()
                    .lineLimit(1)
                    .minimumScaleFactor(0.8)
            }
            value()
                .lineLimit(1)
                .minimumScaleFactor(0.55)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
    }

    /// «июль» — текущий месяц по-русски (для плитки «За июль»).
    static func currentMonthName(for date: Date = Date()) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "ru_RU")
        formatter.dateFormat = "LLLL"
        return formatter.string(from: date)
    }

    // MARK: Топ расходов

    private func topOperationsCard(_ stats: Statistics) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Топ расходов")
                .sectionHeaderStyle()
            VStack(spacing: 0) {
                let top = Array(stats.topOperations.prefix(5))
                ForEach(top) { operation in
                    topOperationRow(operation, currency: stats.currency)
                    if operation.id != top.last?.id {
                        Rectangle()
                            .fill(Color.hairline)
                            .frame(height: 1)
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
    }

    private func topOperationRow(_ operation: TopOperation, currency: String) -> some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(operation.description.isEmpty ? "Расход" : operation.description)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.ink)
                    .lineLimit(1)
                Text("\(operation.donor.displayName) · \(DateFmt.dayMonth(operation.createdAt))")
                    .font(.caption)
                    .foregroundStyle(Color.inkSecondary)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            MoneyText(operation.sum, role: .neutral, size: 15, currency: currency)
        }
        .padding(.vertical, 10)
    }

    // MARK: Загрузка

    private func load() async {
        errorMessage = nil
        do {
            // Через офлайн-кеш: без сети дашборд рисуется по последнему
            // успешному ответу (сетевая ошибка при наличии кеша не всплывает).
            let result = try await session.repo.statistics(roomId: roomId) { [self] cached in
                if stats == nil {
                    stats = cached
                }
            }
            stats = result.value
        } catch {
            // Закрытие sheet посреди запроса — не ошибка (конвенция проекта).
            if error.isTaskCancellation { return }
            errorMessage = error.localizedDescription
        }
    }
}

// MARK: - Общие помощники графиков

/// Цвет участника по карте `MemberPalette.colorIndices`: индекс палитры или
/// `inkSecondary` (7-й и дальше / «Прочие»). Палитра никогда не циклится.
private func memberColor(_ userId: Int?, palette: [Int: Int]) -> Color {
    guard let userId, let index = palette[userId] else { return .inkSecondary }
    return Color.chartCategorical[index]
}

/// Аннотация выбранного столбца: «5 июл — 1 200 ₽» на карточке-подложке
/// (общая для дневного и месячного графиков).
private struct SelectionBadge: View {
    let label: String
    let sum: Int
    let currency: String

    var body: some View {
        HStack(spacing: 4) {
            Text(label)
                .foregroundStyle(Color.inkSecondary)
            Text("—")
                .foregroundStyle(Color.inkSecondary)
            Text(money(sum, currency: currency))
                .fontWeight(.semibold)
                .monospacedDigit()
                .foregroundStyle(Color.ink)
        }
        .font(.system(size: 12, design: .rounded))
        .padding(.horizontal, 8)
        .padding(.vertical, 5)
        .background(Color.surface, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 8, style: .continuous)
                .strokeBorder(Color.hairline, lineWidth: 1)
        }
    }
}

// MARK: - «Динамика по месяцам» (BarMark, 6 месяцев)

/// Столбики трат по календарным месяцам (`byMonth` сервера: 6 месяцев включая
/// текущий, ascending, с нулями). Один оттенок `chartAccent` — это magnitude
/// одной меры, категориальная раскраска тут неуместна. Скругление 4pt,
/// hairline-сетка, выбор столбца — аннотация «фев — 1 200 ₽».
private struct MonthlySpendingCard: View {
    /// Точка графика: русская подпись месяца («фев») и сумма.
    struct MonthPoint: Identifiable {
        let label: String
        let sum: Int
        var id: String { label }
    }

    let points: [MonthPoint]
    let currency: String

    /// Сырой выбор chartXSelection (категория-месяц под пальцем).
    @State private var rawSelection: String?

    init(byMonth: [MonthlySum], currency: String) {
        self.currency = currency
        points = byMonth.map {
            MonthPoint(label: DashboardMath.monthLabel($0.month), sum: $0.sum)
        }
    }

    private var selectedPoint: MonthPoint? {
        guard let rawSelection else { return nil }
        return points.first { $0.label == rawSelection }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Динамика по месяцам")
                .sectionHeaderStyle()
            chart
                .frame(height: 160)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
    }

    private var chart: some View {
        Chart {
            ForEach(points) { point in
                BarMark(
                    x: .value("Месяц", point.label),
                    y: .value("Сумма", point.sum),
                    width: .ratio(0.55)
                )
                .foregroundStyle(Color.chartAccent)
                .cornerRadius(4)
            }
            if let selected = selectedPoint {
                RuleMark(x: .value("Месяц", selected.label))
                    .foregroundStyle(Color.inkSecondary.opacity(0.3))
                    .lineStyle(StrokeStyle(lineWidth: 1))
                    .annotation(
                        position: .top,
                        spacing: 6,
                        overflowResolution: .init(x: .fit(to: .chart), y: .disabled)
                    ) {
                        SelectionBadge(label: selected.label, sum: selected.sum, currency: currency)
                    }
            }
        }
        .chartXSelection(value: $rawSelection)
        // Порядок категорий — календарный (как в данных), не алфавитный.
        .chartXScale(domain: points.map(\.label))
        .chartXAxis {
            AxisMarks { value in
                AxisValueLabel(centered: true) {
                    if let label = value.as(String.self) {
                        Text(label)
                            .font(.system(size: 11, design: .rounded))
                            .foregroundStyle(Color.inkSecondary)
                    }
                }
            }
        }
        .chartYAxis {
            AxisMarks(values: .automatic(desiredCount: 4)) { _ in
                AxisGridLine()
                    .foregroundStyle(Color.hairline)
                AxisValueLabel()
                    .font(.system(size: 11, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
            }
        }
    }
}

// MARK: - «Траты по дням» (BarMark, 30 дней)

/// Столбиковый график трат за последние 30 дней: недостающие дни — нули,
/// тонкие бары `chartAccent` со скруглением 4pt, тихая hairline-сетка,
/// выбор бара (chartXSelection) показывает аннотацию «дата — сумма».
private struct DailySpendingCard: View {
    /// Точка графика: день (начало суток) и сумма трат.
    struct DayPoint: Identifiable {
        let date: Date
        let sum: Int
        var id: Date { date }
    }

    let points: [DayPoint]
    let currency: String

    /// Сырой выбор chartXSelection (дата под пальцем).
    @State private var rawSelection: Date?

    init(byDay: [DailySum], currency: String) {
        self.currency = currency
        points = Self.lastThirtyDays(byDay: byDay)
    }

    /// Ряд из ровно 30 дней (по сегодняшний): дни без трат = 0.
    static func lastThirtyDays(byDay: [DailySum], today: Date = Date()) -> [DayPoint] {
        let calendar = Calendar.current
        let end = calendar.startOfDay(for: today)
        var sums: [Date: Int] = [:]
        for daily in byDay {
            guard let day = daily.day else { continue }
            sums[calendar.startOfDay(for: day), default: 0] += daily.sum
        }
        // От старых к новым: offset 29 (месяц назад) … 0 (сегодня).
        return (0..<30).reversed().compactMap { offset in
            guard let date = calendar.date(byAdding: .day, value: -offset, to: end) else {
                return nil
            }
            return DayPoint(date: date, sum: sums[date] ?? 0)
        }
    }

    /// Выбранная точка: бар того же дня, что и сырое значение выбора.
    private var selectedPoint: DayPoint? {
        guard let rawSelection else { return nil }
        let calendar = Calendar.current
        return points.first { calendar.isDate($0.date, inSameDayAs: rawSelection) }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Траты по дням")
                .sectionHeaderStyle()
            chart
                .frame(height: 180)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
    }

    private var chart: some View {
        Chart {
            ForEach(points) { point in
                BarMark(
                    x: .value("Дата", point.date, unit: .day),
                    y: .value("Сумма", point.sum),
                    width: .ratio(0.55)
                )
                .foregroundStyle(Color.chartAccent)
                .cornerRadius(4)
            }
            if let selected = selectedPoint {
                RuleMark(x: .value("Дата", selected.date, unit: .day))
                    .foregroundStyle(Color.inkSecondary.opacity(0.3))
                    .lineStyle(StrokeStyle(lineWidth: 1))
                    .annotation(
                        position: .top,
                        spacing: 6,
                        overflowResolution: .init(x: .fit(to: .chart), y: .disabled)
                    ) {
                        SelectionBadge(
                            label: DateFmt.dayMonth(selected.date),
                            sum: selected.sum,
                            currency: currency
                        )
                    }
            }
        }
        .chartXSelection(value: $rawSelection)
        .chartXAxis {
            AxisMarks(values: .stride(by: .day, count: 7)) { value in
                AxisValueLabel(centered: true) {
                    if let date = value.as(Date.self) {
                        Text(DateFmt.dayMonth(date))
                            .font(.system(size: 11, design: .rounded))
                            .foregroundStyle(Color.inkSecondary)
                    }
                }
            }
        }
        .chartYAxis {
            AxisMarks(values: .automatic(desiredCount: 4)) { _ in
                AxisGridLine()
                    .foregroundStyle(Color.hairline)
                AxisValueLabel()
                    .font(.system(size: 11, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
            }
        }
    }
}

// MARK: - «Кто платил» (донат SectorMark + легенда)

/// Донат по донорам операций: сегменты цветами участников (identity дублируется
/// легендой, не только цветом), >6 участников — топ-5 + серый «Прочие»
/// (см. `DashboardMath.donutSlices`). В центре — totalSpent мелко; под графиком
/// легенда-столбик: точка 10pt, имя (ink), сумма + процент (inkSecondary),
/// сортировка по убыванию.
private struct PaidDonutCard: View {
    let slices: [DonutSlice]
    let palette: [Int: Int]
    let totalSpent: Int
    let currency: String

    /// База процентов — сумма всех сегментов (все платежи).
    private var total: Int {
        max(slices.reduce(0) { $0 + $1.sum }, 1)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Кто платил")
                .sectionHeaderStyle()
            chart
                .frame(height: 200)
            legend
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
    }

    private var chart: some View {
        Chart(slices) { slice in
            SectorMark(
                angle: .value("Сумма", slice.sum),
                innerRadius: .ratio(0.62),
                angularInset: 1.5
            )
            .foregroundStyle(memberColor(slice.userId, palette: palette))
        }
        .chartLegend(.hidden)
        .chartBackground { proxy in
            GeometryReader { geo in
                if let anchor = proxy.plotFrame {
                    let frame = geo[anchor]
                    VStack(spacing: 2) {
                        Text("всего")
                            .font(.system(size: 11, design: .rounded))
                            .foregroundStyle(Color.inkSecondary)
                        MoneyText(totalSpent, role: .neutral, size: 16, currency: currency)
                            .lineLimit(1)
                            .minimumScaleFactor(0.6)
                    }
                    .frame(maxWidth: min(frame.width, frame.height) * 0.52)
                    .position(x: frame.midX, y: frame.midY)
                }
            }
        }
    }

    private var legend: some View {
        VStack(spacing: 8) {
            ForEach(slices) { slice in
                HStack(spacing: 8) {
                    Circle()
                        .fill(memberColor(slice.userId, palette: palette))
                        .frame(width: 10, height: 10)
                    Text(slice.label)
                        .font(.system(size: 13, weight: .medium, design: .rounded))
                        .foregroundStyle(Color.ink)
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    Text("\(money(slice.sum, currency: currency)) · \(percent(slice))%")
                        .font(.system(size: 12, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(Color.inkSecondary)
                }
            }
        }
    }

    private func percent(_ slice: DonutSlice) -> Int {
        Int((Double(slice.sum) * 100 / Double(total)).rounded())
    }
}

// MARK: - «Чья доля» (горизонтальные BarMark цветами участников)

/// Горизонтальные бары по участникам: каждый бар — цветом СВОЕГО участника
/// (та же карта, что в донате; 7-й и дальше — inkSecondary), имя слева (ink),
/// сумма справа direct-label (inkSecondary), сортировка по убыванию.
private struct MemberBarsCard: View {
    /// Строка графика: подпись участника (уникальная) и сумма.
    struct Bar: Identifiable {
        let id: Int
        let label: String
        let sum: Int
    }

    let title: String
    let bars: [Bar]
    let currency: String
    /// user.id → индекс палитры (см. `MemberPalette.colorIndices`).
    let palette: [Int: Int]

    /// Высота строки участника (28–32pt по спеке дашборда).
    private static let rowHeight: CGFloat = 30

    /// Готовит бары: убирает нули, сортирует по убыванию, делает подписи
    /// уникальными (тёзки получают « (2)») — иначе Charts склеит категории.
    /// nil — рисовать нечего (секция скрывается).
    static func prepared(_ members: [MemberSum]) -> [Bar]? {
        let sorted = members
            .filter { $0.sum != 0 }
            .sorted { $0.sum > $1.sum }
        guard !sorted.isEmpty else { return nil }
        var seen: [String: Int] = [:]
        return sorted.map { member in
            let name = member.user.displayName
            let count = (seen[name] ?? 0) + 1
            seen[name] = count
            return Bar(
                id: member.user.id,
                label: count > 1 ? "\(name) (\(count))" : name,
                sum: member.sum
            )
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title)
                .sectionHeaderStyle()
            chart
                .frame(height: CGFloat(bars.count) * Self.rowHeight)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
    }

    private var chart: some View {
        Chart(bars) { bar in
            BarMark(
                x: .value("Сумма", bar.sum),
                y: .value("Участник", bar.label),
                height: .fixed(16)
            )
            .foregroundStyle(memberColor(bar.id, palette: palette))
            .cornerRadius(4)
            .annotation(
                position: .trailing,
                spacing: 6,
                overflowResolution: .init(x: .fit(to: .chart), y: .disabled)
            ) {
                Text(money(bar.sum, currency: currency))
                    .font(.system(size: 12, weight: .medium, design: .rounded))
                    .monospacedDigit()
                    .foregroundStyle(Color.inkSecondary)
            }
        }
        // Стабильный порядок категорий — по убыванию сумм (как в данных).
        .chartYScale(domain: bars.map(\.label))
        // Ось значений не нужна: суммы подписаны прямо у баров.
        .chartXAxis(.hidden)
        // Запас справа, чтобы direct-label помещался за баром.
        .chartXScale(domain: 0...xUpperBound)
        .chartYAxis {
            AxisMarks(preset: .aligned, position: .leading) { value in
                AxisValueLabel(horizontalSpacing: 8) {
                    if let label = value.as(String.self) {
                        Text(label)
                            .font(.system(size: 13, weight: .medium, design: .rounded))
                            .foregroundStyle(Color.ink)
                            .lineLimit(1)
                    }
                }
            }
        }
        .chartLegend(.hidden)
    }

    /// Верх шкалы X: максимум + треть — место под подпись суммы у бара.
    private var xUpperBound: Int {
        let maxSum = bars.map(\.sum).max() ?? 1
        return max(maxSum + maxSum / 3, 1)
    }
}

// MARK: - «Баланс участников» (diverging бары от нулевой оси)

/// Нетто участников (net = paid − share): положительные бары вправо цветом
/// `accent`, отрицательные влево цветом `negative` — семантические цвета денег
/// (не категориальные), консистентно с остальным приложением. Общая нулевая
/// ось (hairline) через все строки, имя слева, сумма справа (`MoneyText`
/// role .auto: цвет по знаку). Сортировка по net убыванию.
private struct MemberNetCard: View {
    let nets: [MemberNet]
    let currency: String

    private static let rowHeight: CGFloat = 30
    private static let barHeight: CGFloat = 16

    /// Общая шкала всех строк: |net| в долях от maxNegative+maxPositive.
    private var maxPositive: Int { max(nets.map(\.net).max() ?? 0, 0) }
    private var maxNegative: Int { max(-(nets.map(\.net).min() ?? 0), 0) }
    private var span: Int { max(maxPositive + maxNegative, 1) }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Баланс участников")
                .sectionHeaderStyle()
            VStack(spacing: 0) {
                ForEach(nets) { net in
                    row(net)
                        .frame(height: Self.rowHeight)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
    }

    private func row(_ net: MemberNet) -> some View {
        HStack(spacing: 8) {
            Text(net.user.displayName)
                .font(.system(size: 13, weight: .medium, design: .rounded))
                .foregroundStyle(Color.ink)
                .lineLimit(1)
                .frame(width: 88, alignment: .leading)
            divergingBar(net.net)
            MoneyText(net.net, role: .auto, size: 12, currency: currency)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
                .frame(width: 84, alignment: .trailing)
        }
    }

    /// Бар от общей нулевой оси; сама ось — вертикальный hairline на всю
    /// высоту строки (строки без отступов → непрерывная линия через карточку).
    private func divergingBar(_ net: Int) -> some View {
        GeometryReader { geo in
            let width = geo.size.width
            let zeroX = width * CGFloat(maxNegative) / CGFloat(span)
            let barWidth = max(width * CGFloat(abs(net)) / CGFloat(span), net == 0 ? 0 : 2)
            ZStack(alignment: .topLeading) {
                Rectangle()
                    .fill(Color.hairline)
                    .frame(width: 1, height: geo.size.height)
                    .offset(x: zeroX)
                if net != 0 {
                    UnevenRoundedRectangle(
                        topLeadingRadius: net < 0 ? 4 : 0,
                        bottomLeadingRadius: net < 0 ? 4 : 0,
                        bottomTrailingRadius: net > 0 ? 4 : 0,
                        topTrailingRadius: net > 0 ? 4 : 0,
                        style: .continuous
                    )
                    .fill(net > 0 ? Color.accent : Color.negative)
                    .frame(width: barWidth, height: Self.barHeight)
                    .offset(
                        x: net > 0 ? zeroX : zeroX - barWidth,
                        y: (Self.rowHeight - Self.barHeight) / 2
                    )
                }
            }
        }
    }
}

// MARK: - «По дням недели» (BarMark, 7 колонок)

/// Агрегация трат по дню недели (пн…вс): 7 колонок `chartAccent`,
/// столбец-максимум выделен непрозрачностью, значения не подписываются
/// (сетка + оси). Данные — `DashboardMath.weekdayTotals`.
private struct WeekdayCard: View {
    /// Русские короткие подписи, индекс 0 — понедельник.
    static let labels = ["пн", "вт", "ср", "чт", "пт", "сб", "вс"]

    /// 7 сумм, индекс 0 — понедельник … 6 — воскресенье.
    let totals: [Int]

    private var maxSum: Int { totals.max() ?? 0 }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("По дням недели")
                .sectionHeaderStyle()
            chart
                .frame(height: 160)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .surfaceCard()
    }

    private var chart: some View {
        Chart(Array(totals.enumerated()), id: \.offset) { index, sum in
            BarMark(
                x: .value("День", Self.labels[index]),
                y: .value("Сумма", sum),
                width: .ratio(0.55)
            )
            // Максимум — полной непрозрачностью, остальные чуть тише.
            .foregroundStyle(
                sum == maxSum && maxSum > 0
                    ? Color.chartAccent
                    : Color.chartAccent.opacity(0.7)
            )
            .cornerRadius(4)
        }
        // Порядок дней недели — календарный (как в labels), не алфавитный.
        .chartXScale(domain: Self.labels)
        .chartXAxis {
            AxisMarks { value in
                AxisValueLabel(centered: true) {
                    if let label = value.as(String.self) {
                        Text(label)
                            .font(.system(size: 11, design: .rounded))
                            .foregroundStyle(Color.inkSecondary)
                    }
                }
            }
        }
        .chartYAxis {
            AxisMarks(values: .automatic(desiredCount: 4)) { _ in
                AxisGridLine()
                    .foregroundStyle(Color.hairline)
                AxisValueLabel()
                    .font(.system(size: 11, design: .rounded))
                    .foregroundStyle(Color.inkSecondary)
            }
        }
    }
}
