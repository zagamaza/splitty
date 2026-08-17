import SwiftUI

/// Разовое приветствие после первого входа.
///
/// Четыре экрана, и порядок в них не декоративный: сначала что такое группа,
/// потом как вносить расход, потом кто сколько заплатил — и только после этого
/// вывод «платите один раз». Без третьего экрана вывод приходится принимать на
/// веру: суммы переводов берутся из долей, а доли — из расходов.
///
/// Числа выбраны так, чтобы проверяться в уме: 600 на троих — по 200, 300 — по
/// 100. У Бори баланс ровно ноль, поэтому его долг Ане и ваш долг ему
/// схлопываются в один перевод — это и есть механика, показанная арифметикой.
///
/// Иллюстрации живые: каждая начинает движение, когда её страница становится
/// активной (`isActive`), и повторяется. Статичная картинка на онбординге
/// читается как заглушка, а не как рассказ.
struct WelcomeView: View {
    /// Закрыть приветствие. `createGroup` — последний шаг ведёт в создание
    /// группы: приветствие, которое заканчивается пустым экраном, ничего не
    /// изменило.
    let onFinish: (_ createGroup: Bool) -> Void

    @State private var page = 0

    private static let pageCount = 4

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Spacer()
                Button("Пропустить") { onFinish(false) }
                    .scaledFont(size: 16, weight: .medium)
                    .foregroundStyle(Color.inkSecondary)
                    .accessibilityIdentifier("welcomeSkip")
            }
            .padding(.horizontal, 18)
            .padding(.top, 10)

            TabView(selection: $page) {
                ForEach(WelcomeStep.allCases, id: \.rawValue) { step in
                    WelcomeStepView(step: step, isActive: page == step.rawValue)
                        .tag(step.rawValue)
                }
            }
            .tabViewStyle(.page(indexDisplayMode: .never))

            PageDots(count: Self.pageCount, current: page)
                .padding(.top, 16)
                .padding(.bottom, 16)

            Button(page == Self.pageCount - 1 ? "Создать группу" : "Далее") {
                if page == Self.pageCount - 1 {
                    onFinish(true)
                } else {
                    withAnimation(.easeInOut(duration: 0.25)) { page += 1 }
                }
            }
            .buttonStyle(.primaryPill)
            .padding(.horizontal, 20)
            .padding(.bottom, 20)
            .accessibilityIdentifier("welcomePrimary")
        }
        .background(Color.bg)
    }
}

// MARK: - Шаги

/// Шаги приветствия. Отдельным типом — чтобы каждый можно было отрендерить и
/// посмотреть без запуска приложения (`WelcomeRenderTests`).
enum WelcomeStep: Int, CaseIterable {
    case group, dictate, whoPaid, transfers

    var title: String {
        switch self {
        case .group: return String(localized: "Группа — это общий счёт")
        case .dictate: return String(localized: "Скажите — запишем")
        case .whoPaid: return String(localized: "Кто сколько заплатил")
        case .transfers: return String(localized: "Платите один раз")
        }
    }

    var subtitle: String {
        switch self {
        case .group:
            return String(localized: "Поездка, съёмная квартира или один ужин. Платит кто-то один, а расход делят все.")
        case .dictate:
            return String(localized: "Продиктуйте расход вслух, и он появится в группе. Можно снять чек или вписать руками.")
        case .whoPaid:
            return String(localized: "Splitty делит каждый расход на участников. Ужин 600 на троих — по 200, такси 300 — по 100.")
        case .transfers:
            return String(localized: "Ваша доля 300 ₽ уходит одним переводом, а не двумя: Splitty сводит долги внутри группы.")
        }
    }
}

struct WelcomeStepView: View {
    let step: WelcomeStep
    var isActive: Bool = true

    var body: some View {
        WelcomePage(title: step.title, subtitle: step.subtitle) {
            switch step {
            case .group: SharedBillArt(isActive: isActive)
            case .dictate: DictationArt(isActive: isActive)
            case .whoPaid: WhoPaidArt(isActive: isActive)
            case .transfers: TransfersArt(isActive: isActive)
            }
        }
    }
}

// MARK: - Каркас страницы

private struct WelcomePage<Art: View>: View {
    let title: String
    let subtitle: String
    @ViewBuilder let art: () -> Art

    var body: some View {
        VStack(spacing: 0) {
            // Иллюстрация забирает всё свободное место, текст прижат к низу:
            // раньше под текстом пустовала четверть экрана, а картинке этого
            // места как раз не хватало. Содержимое внутри иллюстрации
            // центрируется само, так что растяжение его не разрывает.
            art()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.accent.opacity(0.07), in: RoundedRectangle(cornerRadius: 24, style: .continuous))
                .padding(.horizontal, 16)

            VStack(spacing: 10) {
                Text(title)
                    .scaledFont(size: 26, weight: .bold, relativeTo: .title2)
                    .foregroundStyle(Color.ink)
                    .multilineTextAlignment(.center)
                Text(subtitle)
                    .scaledFont(size: 16, relativeTo: .subheadline)
                    .foregroundStyle(Color.inkSecondary)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(.horizontal, 24)
            .padding(.top, 22)
            .padding(.bottom, 2)
        }
    }
}

private struct PageDots: View {
    let count: Int
    let current: Int

    var body: some View {
        HStack(spacing: 7) {
            ForEach(0..<count, id: \.self) { index in
                Capsule()
                    .fill(index == current ? Color.accent : Color.hairline)
                    .frame(width: index == current ? 22 : 7, height: 7)
                    .animation(.easeInOut(duration: 0.2), value: current)
            }
        }
        .accessibilityHidden(true)
    }
}

// MARK: - Экран 1: расходы падают в общий счёт

private struct SharedBillArt: View {
    let isActive: Bool
    @State private var shown = 3
    @State private var loop: Task<Void, Never>?

    private let slips = [("Ужин", 600), ("Такси", 300), ("Продукты", 450)]

    private var total: String {
        "\(slips.prefix(shown).reduce(0) { $0 + $1.1 }) ₽"
    }

    var body: some View {
        VStack(spacing: 0) {
            welcomeEyebrow("РАСХОДЫ ГРУППЫ")

            Spacer(minLength: 16).frame(maxHeight: 26)

            VStack(spacing: 14) {
                ForEach(Array(slips.enumerated()), id: \.offset) { index, slip in
                    HStack {
                        Text(slip.0).scaledFont(size: 20, weight: .semibold)
                        Spacer()
                        Text("\(slip.1) ₽").font(.system(size: 21, weight: .bold, design: .monospaced))
                    }
                    .padding(.horizontal, 20)
                    .frame(height: 92)
                    .background(Color.surface, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
                    .shadow(color: .black.opacity(0.07), radius: 8, y: 3)
                    .opacity(index < shown ? 1 : 0)
                    .offset(y: index < shown ? 0 : -26)
                    .scaleEffect(index < shown ? 1 : 0.94)
                }
            }

            Spacer(minLength: 16)

            Image(systemName: "arrow.down")
                .font(.system(size: 22, weight: .semibold))
                .foregroundStyle(Color.accent.opacity(0.45))

            Spacer(minLength: 16)

            VStack(spacing: 4) {
                Text("Общий счёт группы")
                    .scaledFont(size: 17, weight: .semibold)
                    .foregroundStyle(Color.accentText)
                Text(total)
                    .font(.system(size: 30, weight: .bold, design: .monospaced))
                    .foregroundStyle(Color.accentText)
                    .contentTransition(.numericText())
            }
            .frame(maxWidth: .infinity)
            .frame(height: 124)
            .overlay(
                RoundedRectangle(cornerRadius: 20, style: .continuous)
                    .strokeBorder(Color.accent.opacity(0.55), style: StrokeStyle(lineWidth: 2, dash: [6, 5]))
            )
            .scaleEffect(shown == slips.count ? 1.03 : 1)
        }
        .padding(22)
        .onAppear { restart() }
        .onDisappear { loop?.cancel() }
        .onChange(of: isActive) { restart() }
    }

    private func restart() {
        loop?.cancel()
        guard isActive else {
            shown = slips.count
            return
        }
        loop = Task { @MainActor in
            while !Task.isCancelled {
                shown = 0
                for index in 1...slips.count {
                    try? await Task.sleep(nanoseconds: 320_000_000)
                    if Task.isCancelled { return }
                    withAnimation(.spring(response: 0.42, dampingFraction: 0.78)) { shown = index }
                }
                try? await Task.sleep(nanoseconds: 1_900_000_000)
                if Task.isCancelled { return }
                withAnimation(.easeIn(duration: 0.3)) { shown = 0 }
                try? await Task.sleep(nanoseconds: 400_000_000)
            }
        }
    }
}

// MARK: - Экран 2: запись голоса и мини-чек

private struct DictationArt: View {
    let isActive: Bool
    @State private var words = 10
    @State private var showReceipt = false
    @State private var pulse = false
    @State private var arc: CGFloat = 0
    @State private var loop: Task<Void, Never>?

    private let phrase = ["пицца", "за", "восемьсот", "и", "кола", "за", "двести", "пополам", "с", "Саней"]

    var body: some View {
        // Фон один на оба состояния: чек проявляется поверх той же тёмной
        // записи, а не подменяет экран вспышкой света.
        ZStack {
            if showReceipt {
                VStack(spacing: 20) {
                    Label("Готово — расход в группе", systemImage: "checkmark.circle.fill")
                        .scaledFont(size: 16, weight: .semibold)
                        .foregroundStyle(.white.opacity(0.85))
                    MiniReceipt()
                }
                .padding(.horizontal, 24)
                .transition(.scale(scale: 0.92).combined(with: .opacity))
            } else {
                recording.transition(.opacity)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(light: 0x2B313A, dark: 0x05080B))
        .onAppear { restart() }
        .onDisappear { loop?.cancel() }
        .onChange(of: isActive) { restart() }
    }

    private var recording: some View {
        // Раскладка та же, что в живом оверлее записи: расшифровка сверху,
        // микрофон внизу под большим пальцем.
        VStack(spacing: 16) {
            Text(phrase.prefix(words).joined(separator: " "))
                .scaledFont(size: 23, weight: .bold)
                .foregroundStyle(.white)
                .multilineTextAlignment(.center)
                .frame(height: 104, alignment: .bottom)
                .padding(.horizontal, 20)

            WelcomeWaveform()

            HStack(spacing: 6) {
                Circle().fill(Color.negative).frame(width: 7, height: 7)
                Text("0:06").font(.system(size: 15, weight: .semibold, design: .monospaced))
                    .foregroundStyle(.white.opacity(0.9))
            }

            Spacer(minLength: 12)

            // Микрофон как в самой записи: пульс-кольцо и дуга лимита в 60 с.
            ZStack {
                Circle()
                    .strokeBorder(Color.accent.opacity(0.55), lineWidth: 3)
                    .frame(width: 104, height: 104)
                    .scaleEffect(pulse ? 1.55 : 1)
                    .opacity(pulse ? 0 : 0.8)

                Circle()
                    .stroke(Color.white.opacity(0.16), lineWidth: 5)
                    .frame(width: 126, height: 126)
                Circle()
                    .trim(from: 0, to: arc)
                    .stroke(Color.white.opacity(0.9), style: StrokeStyle(lineWidth: 5, lineCap: .round))
                    .rotationEffect(.degrees(-90))
                    .frame(width: 126, height: 126)

                Circle().fill(Color.accent).frame(width: 104, height: 104)
                Image(systemName: "mic.fill")
                    .font(.system(size: 40, weight: .semibold))
                    .foregroundStyle(.white)
            }

            Text("Отпустите — распознать · вверх — закрепить")
                .scaledFont(size: 14)
                .foregroundStyle(.white.opacity(0.55))
                .multilineTextAlignment(.center)
                .padding(.horizontal, 16)
        }
        .padding(.vertical, 26)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func restart() {
        loop?.cancel()
        guard isActive else {
            words = phrase.count
            return
        }
        withAnimation(.easeOut(duration: 1.9).repeatForever(autoreverses: false)) { pulse = true }
        loop = Task { @MainActor in
            while !Task.isCancelled {
                withAnimation(.easeOut(duration: 0.25)) { showReceipt = false }
                words = 0
                arc = 0
                withAnimation(.linear(duration: 3.6)) { arc = 0.62 }
                for index in 1...phrase.count {
                    try? await Task.sleep(nanoseconds: 250_000_000)
                    if Task.isCancelled { return }
                    withAnimation(.easeOut(duration: 0.18)) { words = index }
                }
                try? await Task.sleep(nanoseconds: 800_000_000)
                if Task.isCancelled { return }
                withAnimation(.spring(response: 0.45, dampingFraction: 0.8)) { showReceipt = true }
                try? await Task.sleep(nanoseconds: 2_600_000_000)
            }
        }
    }
}

private struct WelcomeWaveform: View {
    @State private var phase = 0

    private let base: [CGFloat] = [10, 26, 40, 19, 32, 46, 15, 29, 37, 21, 11]

    var body: some View {
        HStack(spacing: 5) {
            ForEach(0..<base.count, id: \.self) { index in
                Capsule()
                    .fill(Color.white.opacity(0.92))
                    .frame(width: 4.5, height: base[(index + phase) % base.count])
            }
        }
        .frame(height: 48)
        .task {
            // Волна живая, но нарочно неспешная: экран объясняет, а не пляшет.
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 130_000_000)
                withAnimation(.easeInOut(duration: 0.13)) { phase += 1 }
            }
        }
    }
}

/// Мини-чек — уменьшенный `ReceiptCard` с экрана расхода.
private struct MiniReceipt: View {
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("позиции").font(.system(size: 15, design: .monospaced))
                Spacer()
                Text("2 поз.").font(.system(size: 15, design: .monospaced))
            }
            .foregroundStyle(Color.inkSecondary)

            dashed
            item(name: "Пицца", sum: "800 ₽", each: "по 400 ₽ × 2")
            dashed
            item(name: "Кола", sum: "200 ₽", each: "по 100 ₽ × 2")

            Rectangle().fill(Color.ink).frame(height: 2).padding(.vertical, 14)

            HStack {
                Text("Итого").scaledFont(size: 20, weight: .bold)
                Spacer()
                Text("1000 ₽").font(.system(size: 24, weight: .bold, design: .monospaced))
            }
        }
        .padding(20)
        .background(Color.receiptPaper)
        .clipShape(RoundedRectangle(cornerRadius: 4, style: .continuous))
        .shadow(color: .black.opacity(0.2), radius: 16, y: 8)
    }

    private var dashed: some View {
        Rectangle().fill(Color.hairline).frame(height: 1).padding(.vertical, 10)
    }

    private func item(name: String, sum: String, each: String) -> some View {
        VStack(spacing: 10) {
            HStack {
                Text(name).scaledFont(size: 19, weight: .bold)
                Spacer()
                Text(sum).font(.system(size: 19, weight: .bold, design: .monospaced))
            }
            HStack {
                HStack(spacing: -6) {
                    receiptAvatar("Я", color: .accent)
                    receiptAvatar("С", color: Color(red: 0.55, green: 0.36, blue: 0.96))
                }
                Spacer()
                Text(each).font(.system(size: 14, design: .monospaced)).foregroundStyle(Color.inkSecondary)
            }
        }
    }

    private func receiptAvatar(_ letter: String, color: Color) -> some View {
        Text(letter)
            .font(.system(size: 11, weight: .bold))
            .foregroundStyle(.white)
            .frame(width: 24, height: 24)
            .background(color, in: Circle())
            .overlay(Circle().strokeBorder(Color.receiptPaper, lineWidth: 2))
    }
}

// MARK: - Экран 3: кто сколько заплатил

private struct WhoPaidArt: View {
    let isActive: Bool
    @State private var shown = 3
    @State private var loop: Task<Void, Never>?

    var body: some View {
        VStack(spacing: 14) {
            welcomeEyebrow("КТО ЗА ЧТО ЗАПЛАТИЛ")

            Spacer(minLength: 10).frame(maxHeight: 22)

            paidCard(
                initial: "А", name: "Аня", what: "за ужин", sum: "600 ₽", share: "по 200 ₽",
                color: .accent
            )
            .opacity(shown > 0 ? 1 : 0)
            .offset(y: shown > 0 ? 0 : -18)

            paidCard(
                initial: "Б", name: "Боря", what: "за такси", sum: "300 ₽", share: "по 100 ₽",
                color: Color(red: 0.18, green: 0.43, blue: 0.89)
            )
            .opacity(shown > 1 ? 1 : 0)
            .offset(y: shown > 1 ? 0 : -18)

            Spacer(minLength: 12)

            Image(systemName: "arrow.down")
                .font(.system(size: 22, weight: .semibold))
                .foregroundStyle(Color.accent.opacity(0.45))
                .opacity(shown > 2 ? 1 : 0)

            Spacer(minLength: 12)

            VStack(spacing: 14) {
                HStack(spacing: 12) {
                    welcomeAvatar("Я", color: Color.inkSecondary, size: 48)
                    VStack(alignment: .leading, spacing: 3) {
                        Text("Вы").scaledFont(size: 20, weight: .semibold)
                        Text("не платили")
                            .scaledFont(size: 15)
                            .foregroundStyle(Color.inkSecondary)
                    }
                    Spacer(minLength: 0)
                    Text("300 ₽")
                        .font(.system(size: 26, weight: .bold, design: .monospaced))
                        .foregroundStyle(Color.accentText)
                }

                // Сумма долей выписана, чтобы 300 ₽ можно было проверить в уме.
                HStack {
                    Text("ваша доля: 200 + 100")
                        .font(.system(size: 15, design: .monospaced))
                        .foregroundStyle(Color.accentText.opacity(0.75))
                    Spacer(minLength: 0)
                }
                .padding(.top, 14)
                .overlay(alignment: .top) { Rectangle().fill(Color.accent.opacity(0.25)).frame(height: 1) }
            }
            .padding(20)
            .background(Color.accent.opacity(0.11), in: RoundedRectangle(cornerRadius: 20, style: .continuous))
            .opacity(shown > 2 ? 1 : 0)
            .scaleEffect(shown > 2 ? 1 : 0.96)
        }
        .padding(18)
        .onAppear { restart() }
        .onDisappear { loop?.cancel() }
        .onChange(of: isActive) { restart() }
    }

    private func restart() {
        loop?.cancel()
        guard isActive else {
            shown = 3
            return
        }
        loop = Task { @MainActor in
            while !Task.isCancelled {
                shown = 0
                for index in 1...3 {
                    try? await Task.sleep(nanoseconds: index == 3 ? 520_000_000 : 380_000_000)
                    if Task.isCancelled { return }
                    withAnimation(.spring(response: 0.45, dampingFraction: 0.8)) { shown = index }
                }
                try? await Task.sleep(nanoseconds: 2_400_000_000)
                if Task.isCancelled { return }
                withAnimation(.easeIn(duration: 0.28)) { shown = 0 }
                try? await Task.sleep(nanoseconds: 400_000_000)
            }
        }
    }

    private func paidCard(
        initial: String, name: String, what: String, sum: String, share: String, color: Color
    ) -> some View {
        VStack(spacing: 14) {
            HStack(spacing: 12) {
                welcomeAvatar(initial, color: color, size: 48)
                VStack(alignment: .leading, spacing: 2) {
                    Text(name).scaledFont(size: 20, weight: .semibold)
                    Text(what).scaledFont(size: 15).foregroundStyle(Color.inkSecondary)
                }
                Spacer(minLength: 0)
                Text(sum).font(.system(size: 26, weight: .bold, design: .monospaced))
            }

            HStack(spacing: 8) {
                Text("делим на троих").scaledFont(size: 16).foregroundStyle(Color.inkSecondary)
                Text(share)
                    .font(.system(size: 16, weight: .bold, design: .monospaced))
                    .foregroundStyle(Color.accentText)
                    .padding(.horizontal, 13)
                    .padding(.vertical, 7)
                    .background(Color.accent.opacity(0.14), in: Capsule())
                Spacer(minLength: 0)
            }
            .padding(.top, 14)
            .overlay(alignment: .top) { Rectangle().fill(Color.hairline).frame(height: 1) }
        }
        .padding(20)
        .background(Color.surface, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .shadow(color: .black.opacity(0.07), radius: 8, y: 3)
    }
}

// MARK: - Экран 4: сколько раз платить

/// Сравнение «Без Splitty / Со Splitty». Темп задаёт человек — но один раз
/// переключатель срабатывает сам, иначе половина людей не поймёт, что тут есть
/// что нажать, и увидит только состояние «без».
private struct TransfersArt: View {
    let isActive: Bool
    @State private var withSplitty = false
    @State private var demo: Task<Void, Never>?

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack {
                Text("Ваша доля за вечер").scaledFont(size: 17, weight: .semibold)
                Spacer(minLength: 0)
                Text("300 ₽").font(.system(size: 24, weight: .bold, design: .monospaced))
            }
            .padding(.horizontal, 18)
            .frame(height: 76)
            .background(Color.surface, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
            .shadow(color: .black.opacity(0.07), radius: 8, y: 3)

            Spacer(minLength: 12).frame(maxHeight: 24)

            welcomeEyebrow("ВАМ ПЕРЕВОДИТЬ")

            payRow(
                initial: "А", name: "Ане",
                note: withSplitty ? "200 + 100 Бори" : "за ужин",
                sum: withSplitty ? "300 ₽" : "200 ₽", color: .accent
            )

            // Место под вторую строку занято в обоих состояниях: слева перевод,
            // справа объяснение, почему его больше нет. Иначе при переключении
            // низ экрана прыгает, а половина карточки пустует.
            ZStack(alignment: .top) {
                if !withSplitty {
                    VStack(alignment: .leading, spacing: 10) {
                        payRow(initial: "Б", name: "Боре", note: "за такси", sum: "100 ₽",
                               color: Color(red: 0.18, green: 0.43, blue: 0.89))
                        sideNote("Два перевода, два подтверждения", color: .negative)
                    }
                    .transition(.opacity)
                } else {
                    VStack(alignment: .leading, spacing: 10) {
                        settledRow
                        sideNote("Его 100 ₽ уходят Ане вместе с вашими", color: .accentText)
                    }
                    .transition(.opacity)
                }
            }
            .frame(height: 168, alignment: .top)

            Spacer(minLength: 12)

            HStack(spacing: 10) {
                Spacer(minLength: 0)
                ForEach(0..<2, id: \.self) { index in
                    RoundedRectangle(cornerRadius: 3)
                        .fill(mark(at: index))
                        .frame(width: 15, height: 22)
                }
                Text(withSplitty ? "1 перевод" : "2 перевода")
                    .scaledFont(size: 18, weight: .bold)
                    .foregroundStyle(withSplitty ? Color.accentText : Color.negative)
                Spacer(minLength: 0)
            }
            .padding(.vertical, 16)
            .background(
                (withSplitty ? Color.accent : Color.negative).opacity(0.1),
                in: RoundedRectangle(cornerRadius: 18, style: .continuous)
            )

            // Свой сегмент, а не системный Picker: системный тянет чужую
            // типографику и серую заливку — рядом с карточками приложения он
            // выглядит вставленным из другой программы.
            HStack(spacing: 4) {
                segmentHalf("Без Splitty", isOn: !withSplitty) { set(false) }
                segmentHalf("Со Splitty", isOn: withSplitty) { set(true) }
            }
            .padding(5)
            .background(Color.ink.opacity(0.06), in: Capsule())
            .accessibilityIdentifier("welcomeCompare")
        }
        .padding(18)
        .animation(.spring(response: 0.42, dampingFraction: 0.82), value: withSplitty)
        .onAppear { startDemo() }
        .onDisappear { demo?.cancel() }
        .onChange(of: isActive) { startDemo() }
    }

    private func set(_ value: Bool) {
        demo?.cancel()
        withSplitty = value
    }

    private func startDemo() {
        demo?.cancel()
        guard isActive else { return }
        demo = Task { @MainActor in
            withSplitty = false
            try? await Task.sleep(nanoseconds: 1_700_000_000)
            if Task.isCancelled { return }
            withSplitty = true
        }
    }

    private func mark(at index: Int) -> Color {
        if withSplitty { return index == 0 ? Color.accent : Color.hairline }
        return Color.negative
    }

    /// Боря заплатил ровно свою долю: его баланс ноль, поэтому строка «вам
    /// переводить» для него исчезает — это и есть сведение долгов.
    private var settledRow: some View {
        HStack(spacing: 12) {
            welcomeAvatar("Б", color: Color(red: 0.18, green: 0.43, blue: 0.89).opacity(0.35), size: 54)
            VStack(alignment: .leading, spacing: 2) {
                Text("Боре").scaledFont(size: 21, weight: .semibold)
                    .foregroundStyle(Color.inkSecondary)
                Text("в расчёте").scaledFont(size: 15).foregroundStyle(Color.inkSecondary)
            }
            Spacer(minLength: 0)
            Text("0 ₽")
                .font(.system(size: 26, weight: .bold, design: .monospaced))
                .foregroundStyle(Color.accentText)
        }
        .padding(.horizontal, 18)
        .frame(height: 112)
        .background(Color.accent.opacity(0.12), in: RoundedRectangle(cornerRadius: 22, style: .continuous))
    }

    private func sideNote(_ text: String, color: Color) -> some View {
        Text(text)
            .scaledFont(size: 15)
            .foregroundStyle(color)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func payRow(initial: String, name: String, note: String, sum: String, color: Color) -> some View {
        HStack(spacing: 12) {
            welcomeAvatar(initial, color: color, size: 54)
            VStack(alignment: .leading, spacing: 2) {
                Text(name).scaledFont(size: 21, weight: .semibold)
                Text(note).scaledFont(size: 15).foregroundStyle(Color.inkSecondary)
            }
            Spacer(minLength: 0)
            Text(sum)
                .font(.system(size: 26, weight: .bold, design: .monospaced))
                .foregroundStyle(withSplitty ? Color.accentText : Color.negative)
                .contentTransition(.numericText())
        }
        .padding(.horizontal, 18)
        .frame(height: 112)
        .background(Color.surface, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
        .shadow(color: .black.opacity(0.07), radius: 8, y: 3)
    }

    private func segmentHalf(_ title: String, isOn: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .scaledFont(size: 17, weight: .semibold)
                .foregroundStyle(isOn ? .white : Color.inkSecondary)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 17)
                .background(isOn ? Color.accent : .clear, in: Capsule())
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Общее

private func welcomeEyebrow(_ text: String) -> some View {
    Text(text)
        .font(.system(size: 13.5, weight: .semibold, design: .monospaced))
        .foregroundStyle(Color.inkSecondary)
        .frame(maxWidth: .infinity, alignment: .leading)
}

private func welcomeAvatar(_ letter: String, color: Color, size: CGFloat) -> some View {
    Text(letter)
        .font(.system(size: size * 0.42, weight: .bold))
        .foregroundStyle(.white)
        .frame(width: size, height: size)
        .background(color, in: Circle())
}
