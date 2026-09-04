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
                Button("Пропустить") {
                    Analytics.shared.track(.onboardingSkipped)
                    onFinish(false)
                }
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
                    Analytics.shared.track(.onboardingCompleted)
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
        .onAppear { Analytics.shared.track(.onboardingStarted) }
        .onChange(of: page) { _, current in
            guard let step = WelcomeStep(rawValue: current) else { return }
            Analytics.shared.track(.onboardingStep(step: step.analyticsName))
        }
    }
}

// MARK: - Шаги

/// Шаги приветствия. Отдельным типом — чтобы каждый можно было отрендерить и
/// посмотреть без запуска приложения (`WelcomeRenderTests`).
enum WelcomeStep: Int, CaseIterable {
    case group, dictate, whoPaid, transfers

    /// Имя шага в контракте событий. Отдельно от `case`: имена там snake_case и
    /// общие со вторым клиентом, а переименование шага в коде не должно тихо
    /// разводить один шаг воронки на два.
    var analyticsName: String {
        switch self {
        case .group: return "group"
        case .dictate: return "dictate"
        case .whoPaid: return "who_paid"
        case .transfers: return "transfers"
        }
    }

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

    private let slips = [("Ужин", 600), ("Такси", 300), ("Продукты", 450), ("Кофе", 150)]

    private var total: String {
        "\(slips.prefix(shown).reduce(0) { $0 + $1.1 }) ₽"
    }

    var body: some View {
        // Композиция собрана компактно и стоит по центру: тянуть строки
        // распорками по всей карточке — значит порвать список на куски.
        VStack(spacing: 0) {
            welcomeEyebrow("РАСХОДЫ ГРУППЫ")
                .padding(.bottom, 14)

            VStack(spacing: 10) {
                ForEach(Array(slips.enumerated()), id: \.offset) { index, slip in
                    HStack(spacing: 14) {
                        Text(slip.0)
                            .scaledFont(size: 16, weight: .semibold)
                            .foregroundStyle(Color.ink)
                        Spacer(minLength: 8)
                        Text("\(slip.1) ₽")
                            .font(.system(size: 17, weight: .semibold, design: .monospaced))
                            .foregroundStyle(Color.ink)
                    }
                    .surfaceCard()
                    .opacity(index < shown ? 1 : 0)
                    .offset(y: index < shown ? 0 : -22)
                    .scaleEffect(index < shown ? 1 : 0.96)
                }
            }

            Image(systemName: "arrow.down")
                .font(.system(size: 18, weight: .semibold))
                .foregroundStyle(Color.accent.opacity(0.45))
                .padding(.vertical, 14)

            VStack(spacing: 2) {
                Text("Общий счёт группы")
                    .scaledFont(size: 14, weight: .medium)
                    .foregroundStyle(Color.accentText.opacity(0.8))
                Text(total)
                    .font(.system(size: 22, weight: .bold, design: .monospaced))
                    .foregroundStyle(Color.accentText)
                    .contentTransition(.numericText())
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 18)
            .overlay(
                RoundedRectangle(cornerRadius: 16, style: .continuous)
                    .strokeBorder(Color.accent.opacity(0.5), style: StrokeStyle(lineWidth: 1.5, dash: [6, 5]))
            )
        }
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
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
                    try? await Task.sleep(nanoseconds: 600_000_000)
                    if Task.isCancelled { return }
                    withAnimation(.spring(response: 0.42, dampingFraction: 0.78)) { shown = index }
                }
                // Собранный счёт держим долго: это и есть мысль экрана, а не
                // мельтешение появлений.
                try? await Task.sleep(nanoseconds: 4_500_000_000)
                if Task.isCancelled { return }
                withAnimation(.easeIn(duration: 0.35)) { shown = 0 }
                try? await Task.sleep(nanoseconds: 700_000_000)
            }
        }
    }
}

// MARK: - Экран 2: запись голоса, распознавание и мини-чек

/// Повторяет живой `RecordingOverlay` и `parsingOverlay` с экрана расхода:
/// те же размеры микрофона и волны, тот же счётчик, те же тексты статуса.
/// Онбординг обещает ровно тот экран, который человек увидит.
private struct DictationArt: View {
    let isActive: Bool

    enum Stage { case recording, parsing, receipt }

    @State private var stage: Stage = .recording
    @State private var words = 10
    @State private var seconds = 6
    @State private var pulse = false
    @State private var arc: CGFloat = 0
    @State private var loop: Task<Void, Never>?

    private let phrase = ["пицца", "за", "восемьсот", "и", "кола", "за", "двести", "пополам", "с", "Саней"]

    var body: some View {
        // Фон один на все стадии: распознавание и чек проявляются поверх той же
        // тёмной записи, а не подменяют экран вспышкой света.
        ZStack {
            switch stage {
            case .recording:
                recording.transition(.opacity)
            case .parsing:
                parsing.transition(.opacity)
            case .receipt:
                receipt.transition(.scale(scale: 0.94).combined(with: .opacity))
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(light: 0x2B313A, dark: 0x05080B))
        .onAppear { restart() }
        .onDisappear { loop?.cancel() }
        .onChange(of: isActive) { restart() }
    }

    // MARK: Стадии

    private var recording: some View {
        VStack(spacing: 0) {
            Spacer(minLength: 8)

            // Окно расшифровки: 21pt и маска сверху — как в оверлее записи.
            Text(phrase.prefix(words).joined(separator: " "))
                .scaledFont(size: 21, weight: .semibold)
                .foregroundStyle(.white)
                .multilineTextAlignment(.center)
                .lineSpacing(4)
                .frame(maxWidth: 320)
                .frame(height: 96, alignment: .bottom)
                .clipped()
                .mask(
                    LinearGradient(
                        stops: [
                            .init(color: .clear, location: 0),
                            .init(color: .black, location: 0.4),
                            .init(color: .black, location: 1),
                        ],
                        startPoint: .top, endPoint: .bottom
                    )
                )

            WelcomeWaveform()
                .frame(width: 240, height: 44)
                .padding(.top, 16)

            HStack(spacing: 8) {
                Circle()
                    .fill(Color.negative)
                    .frame(width: 8, height: 8)
                    .opacity(seconds % 2 == 0 ? 1 : 0.25)
                Text(String(format: "0:%02d", seconds))
                    .scaledFont(size: 18, weight: .bold)
                    .monospacedDigit()
                    .foregroundStyle(.white)
            }
            .padding(.top, 12)

            VStack(spacing: 6) {
                Text("Говорите…")
                    .scaledFont(size: 20, weight: .bold)
                    .foregroundStyle(.white)
                Text("Отпустите — распознать · вверх — закрепить")
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(.white.opacity(0.75))
                    .multilineTextAlignment(.center)
            }
            .padding(.top, 12)

            Spacer(minLength: 20)

            micCircle
        }
        .padding(.horizontal, 24)
        .padding(.vertical, 22)
    }

    /// Микрофон 82 pt — ровно кнопка записи из формы расхода.
    private var micCircle: some View {
        ZStack {
            Circle()
                .strokeBorder(Color.accent.opacity(0.5), lineWidth: 2)
                .frame(width: 82, height: 82)
                .scaleEffect(pulse ? 1.5 : 1)
                .opacity(pulse ? 0 : 0.8)

            Circle()
                .stroke(Color.white.opacity(0.16), lineWidth: 4)
                .frame(width: 98, height: 98)
            Circle()
                .trim(from: 0, to: arc)
                .stroke(Color.white.opacity(0.9), style: StrokeStyle(lineWidth: 4, lineCap: .round))
                .rotationEffect(.degrees(-90))
                .frame(width: 98, height: 98)

            Circle().fill(Color.accent).frame(width: 82, height: 82)
            Image(systemName: "mic.fill")
                .font(.system(size: 30, weight: .semibold))
                .foregroundStyle(.white)
        }
    }

    /// Стадия распознавания — тот же текст, что в живом `parsingOverlay`.
    private var parsing: some View {
        VStack(spacing: 18) {
            ProgressView()
                .controlSize(.large)
                .tint(.white)
            Text("Распознаю…")
                .scaledFont(size: 17, weight: .semibold)
                .foregroundStyle(.white)
            Text("Считываю расход и раскладываю по позициям")
                .scaledFont(size: 13, relativeTo: .footnote)
                .foregroundStyle(.white.opacity(0.7))
                .multilineTextAlignment(.center)
                .padding(.horizontal, 40)
        }
    }

    private var receipt: some View {
        VStack(spacing: 16) {
            Label("Готово — расход в группе", systemImage: "checkmark.circle.fill")
                .scaledFont(size: 15, weight: .semibold)
                .foregroundStyle(.white.opacity(0.85))
            MiniReceipt()
        }
        .padding(.horizontal, 28)
    }

    // MARK: Цикл

    private func restart() {
        loop?.cancel()
        guard isActive else {
            words = phrase.count
            seconds = 6
            stage = .recording
            return
        }
        withAnimation(.easeOut(duration: 1.4).repeatForever(autoreverses: false)) { pulse = true }
        loop = Task { @MainActor in
            while !Task.isCancelled {
                withAnimation(.easeOut(duration: 0.25)) { stage = .recording }
                words = 0
                seconds = 0
                arc = 0
                withAnimation(.linear(duration: 4.5)) { arc = 0.11 }
                // Слова и секунды идут вместе: 4,5 с записи — столько же, сколько
                // человек реально говорит эту фразу.
                for index in 1...phrase.count {
                    try? await Task.sleep(nanoseconds: 450_000_000)
                    if Task.isCancelled { return }
                    withAnimation(.easeOut(duration: 0.18)) { words = index }
                    if index % 2 == 0 { seconds += 1 }
                }
                try? await Task.sleep(nanoseconds: 700_000_000)
                if Task.isCancelled { return }
                withAnimation(.easeOut(duration: 0.25)) { stage = .parsing }
                try? await Task.sleep(nanoseconds: 1_600_000_000)
                if Task.isCancelled { return }
                withAnimation(.spring(response: 0.45, dampingFraction: 0.85)) { stage = .receipt }
                try? await Task.sleep(nanoseconds: 3_800_000_000)
            }
        }
    }
}

private struct WelcomeWaveform: View {
    @State private var phase = 0

    private let base: [CGFloat] = [8, 20, 30, 15, 24, 34, 12, 22, 28, 16, 9, 26, 14, 32, 18]

    var body: some View {
        HStack(spacing: 4) {
            ForEach(0..<base.count, id: \.self) { index in
                Capsule()
                    .fill(Color.white.opacity(0.92))
                    .frame(width: 4, height: base[(index + phase) % base.count])
            }
        }
        .task {
            // Волна живая, но нарочно неспешная: экран объясняет, а не пляшет.
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 150_000_000)
                withAnimation(.easeInOut(duration: 0.15)) { phase += 1 }
            }
        }
    }
}

/// Мини-чек — уменьшенный `ReceiptCard` с экрана расхода.
private struct MiniReceipt: View {
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("позиции").font(.system(size: 12, design: .monospaced))
                Spacer()
                Text("2 поз.").font(.system(size: 12, design: .monospaced))
            }
            .foregroundStyle(Color.inkSecondary)

            dashed
            item(name: "Пицца", sum: "800 ₽", each: "по 400 ₽ × 2")
            dashed
            item(name: "Кола", sum: "200 ₽", each: "по 100 ₽ × 2")

            Rectangle().fill(Color.ink).frame(height: 1.5).padding(.vertical, 10)

            HStack {
                Text("Итого").scaledFont(size: 16, weight: .bold)
                Spacer()
                Text("1000 ₽").font(.system(size: 17, weight: .bold, design: .monospaced))
            }
        }
        .padding(16)
        .background(Color.receiptPaper)
        .clipShape(RoundedRectangle(cornerRadius: 4, style: .continuous))
        .shadow(color: .black.opacity(0.25), radius: 16, y: 8)
    }

    private var dashed: some View {
        Rectangle().fill(Color.hairline).frame(height: 1).padding(.vertical, 9)
    }

    private func item(name: String, sum: String, each: String) -> some View {
        VStack(spacing: 8) {
            HStack {
                Text(name).scaledFont(size: 15, weight: .semibold)
                Spacer()
                Text(sum).font(.system(size: 15, weight: .semibold, design: .monospaced))
            }
            HStack {
                HStack(spacing: -6) {
                    receiptAvatar("Я", color: .accent)
                    receiptAvatar("С", color: Color(red: 0.55, green: 0.36, blue: 0.96))
                }
                Spacer()
                Text(each).font(.system(size: 12, design: .monospaced)).foregroundStyle(Color.inkSecondary)
            }
        }
    }

    private func receiptAvatar(_ letter: String, color: Color) -> some View {
        Text(letter)
            .font(.system(size: 10, weight: .bold))
            .foregroundStyle(.white)
            .frame(width: 20, height: 20)
            .background(color, in: Circle())
            .overlay(Circle().strokeBorder(Color.receiptPaper, lineWidth: 2))
    }
}

// MARK: - Экран 3: кто за что заплатил

private struct WhoPaidArt: View {
    let isActive: Bool
    @State private var shown = 3
    @State private var loop: Task<Void, Never>?

    var body: some View {
        VStack(spacing: 12) {
            welcomeEyebrow("КТО ЗА ЧТО ЗАПЛАТИЛ")
                .padding(.bottom, 2)

            paidCard(
                initial: "А", name: "Аня", what: "за ужин", sum: "600 ₽", share: "по 200 ₽",
                color: .accent
            )
            .opacity(shown > 0 ? 1 : 0)
            .offset(y: shown > 0 ? 0 : -16)

            paidCard(
                initial: "Б", name: "Боря", what: "за такси", sum: "300 ₽", share: "по 100 ₽",
                color: Color(red: 0.18, green: 0.43, blue: 0.89)
            )
            .opacity(shown > 1 ? 1 : 0)
            .offset(y: shown > 1 ? 0 : -16)

            Image(systemName: "arrow.down")
                .font(.system(size: 18, weight: .semibold))
                .foregroundStyle(Color.accent.opacity(0.45))
                .opacity(shown > 2 ? 1 : 0)
                .padding(.vertical, 2)

            VStack(spacing: 12) {
                HStack(spacing: 14) {
                    welcomeAvatar("Я", color: Color.inkSecondary, size: 46)
                    VStack(alignment: .leading, spacing: 3) {
                        Text("Вы").scaledFont(size: 16, weight: .semibold)
                        Text("не платили")
                            .font(.system(size: 13))
                            .foregroundStyle(Color.inkSecondary)
                    }
                    Spacer(minLength: 8)
                    Text("300 ₽")
                        .font(.system(size: 17, weight: .semibold, design: .monospaced))
                        .foregroundStyle(Color.accentText)
                }

                // Сумма долей выписана, чтобы 300 ₽ можно было проверить в уме.
                HStack {
                    Text("ваша доля: 200 + 100")
                        .font(.system(size: 13, design: .monospaced))
                        .foregroundStyle(Color.accentText.opacity(0.75))
                    Spacer(minLength: 0)
                }
                .padding(.top, 12)
                .overlay(alignment: .top) { Rectangle().fill(Color.accent.opacity(0.25)).frame(height: 1) }
            }
            .padding(16)
            .background(Color.accent.opacity(0.11), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
            .opacity(shown > 2 ? 1 : 0)
            .scaleEffect(shown > 2 ? 1 : 0.97)
        }
        .padding(18)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
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
                    // Каждую карточку надо успеть прочитать: суммы здесь и есть
                    // содержание экрана.
                    try? await Task.sleep(nanoseconds: index == 3 ? 1_400_000_000 : 1_100_000_000)
                    if Task.isCancelled { return }
                    withAnimation(.spring(response: 0.45, dampingFraction: 0.85)) { shown = index }
                }
                try? await Task.sleep(nanoseconds: 5_000_000_000)
                if Task.isCancelled { return }
                withAnimation(.easeIn(duration: 0.35)) { shown = 0 }
                try? await Task.sleep(nanoseconds: 700_000_000)
            }
        }
    }

    private func paidCard(
        initial: String, name: String, what: String, sum: String, share: String, color: Color
    ) -> some View {
        VStack(spacing: 12) {
            HStack(spacing: 14) {
                welcomeAvatar(initial, color: color, size: 46)
                VStack(alignment: .leading, spacing: 3) {
                    Text(name).scaledFont(size: 16, weight: .semibold)
                    Text(what).font(.system(size: 13)).foregroundStyle(Color.inkSecondary)
                }
                Spacer(minLength: 8)
                Text(sum).font(.system(size: 17, weight: .semibold, design: .monospaced))
            }

            HStack(spacing: 8) {
                Text("делим на троих").font(.system(size: 13)).foregroundStyle(Color.inkSecondary)
                Text(share)
                    .font(.system(size: 13, weight: .semibold, design: .monospaced))
                    .foregroundStyle(Color.accentText)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 5)
                    .background(Color.accent.opacity(0.14), in: Capsule())
                Spacer(minLength: 0)
            }
            .padding(.top, 12)
            .overlay(alignment: .top) { Rectangle().fill(Color.hairline).frame(height: 1) }
        }
        .surfaceCard()
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
        VStack(alignment: .leading, spacing: 12) {
            HStack {
                Text("Ваша доля за вечер").scaledFont(size: 16, weight: .semibold)
                Spacer(minLength: 8)
                Text("300 ₽").font(.system(size: 17, weight: .semibold, design: .monospaced))
            }
            .surfaceCard()
            .padding(.bottom, 6)

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
                    VStack(alignment: .leading, spacing: 8) {
                        payRow(initial: "Б", name: "Боре", note: "за такси", sum: "100 ₽",
                               color: Color(red: 0.18, green: 0.43, blue: 0.89))
                        sideNote("Два перевода, два подтверждения", color: .negative)
                    }
                    .transition(.opacity)
                } else {
                    VStack(alignment: .leading, spacing: 8) {
                        settledRow
                        sideNote("Его 100 ₽ уходят Ане вместе с вашими", color: .accentText)
                    }
                    .transition(.opacity)
                }
            }
            .frame(height: 108, alignment: .top)
            .padding(.bottom, 6)

            HStack(spacing: 8) {
                Spacer(minLength: 0)
                ForEach(0..<2, id: \.self) { index in
                    RoundedRectangle(cornerRadius: 2.5)
                        .fill(mark(at: index))
                        .frame(width: 11, height: 15)
                }
                Text(withSplitty ? "1 перевод" : "2 перевода")
                    .scaledFont(size: 15, weight: .semibold)
                    .foregroundStyle(withSplitty ? Color.accentText : Color.negative)
                Spacer(minLength: 0)
            }
            .padding(.vertical, 12)
            .background(
                (withSplitty ? Color.accent : Color.negative).opacity(0.1),
                in: RoundedRectangle(cornerRadius: 14, style: .continuous)
            )

            // Свой сегмент, а не системный Picker: системный тянет чужую
            // типографику и серую заливку — рядом с карточками приложения он
            // выглядит вставленным из другой программы.
            HStack(spacing: 4) {
                segmentHalf("Без Splitty", isOn: !withSplitty) { set(false) }
                segmentHalf("Со Splitty", isOn: withSplitty) { set(true) }
            }
            .padding(4)
            .background(Color.ink.opacity(0.06), in: Capsule())
            .accessibilityIdentifier("welcomeCompare")
        }
        .padding(18)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .animation(.spring(response: 0.45, dampingFraction: 0.85), value: withSplitty)
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
            // Пауза длиннее: сначала надо прочитать «без», иначе переключение
            // происходит раньше, чем человек понял, что сравнивают.
            try? await Task.sleep(nanoseconds: 2_800_000_000)
            if Task.isCancelled { return }
            withSplitty = true
        }
    }

    private func mark(at index: Int) -> Color {
        if withSplitty { return index == 0 ? Color.accent : Color.hairline }
        return Color.negative
    }

    private func sideNote(_ text: String, color: Color) -> some View {
        Text(text)
            .font(.system(size: 13))
            .foregroundStyle(color)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// Боря заплатил ровно свою долю: его баланс ноль, поэтому строка «вам
    /// переводить» для него исчезает — это и есть сведение долгов.
    private var settledRow: some View {
        HStack(spacing: 14) {
            welcomeAvatar("Б", color: Color(red: 0.18, green: 0.43, blue: 0.89).opacity(0.35), size: 46)
            VStack(alignment: .leading, spacing: 3) {
                Text("Боре").scaledFont(size: 16, weight: .semibold).foregroundStyle(Color.inkSecondary)
                Text("в расчёте").font(.system(size: 13)).foregroundStyle(Color.inkSecondary)
            }
            Spacer(minLength: 8)
            Text("0 ₽")
                .font(.system(size: 17, weight: .semibold, design: .monospaced))
                .foregroundStyle(Color.accentText)
        }
        .padding(16)
        .background(Color.accent.opacity(0.12), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
    }

    private func payRow(initial: String, name: String, note: String, sum: String, color: Color) -> some View {
        HStack(spacing: 14) {
            welcomeAvatar(initial, color: color, size: 46)
            VStack(alignment: .leading, spacing: 3) {
                Text(name).scaledFont(size: 16, weight: .semibold)
                Text(note).font(.system(size: 13)).foregroundStyle(Color.inkSecondary)
            }
            Spacer(minLength: 8)
            Text(sum)
                .font(.system(size: 17, weight: .semibold, design: .monospaced))
                .foregroundStyle(withSplitty ? Color.accentText : Color.negative)
                .contentTransition(.numericText())
        }
        .surfaceCard()
    }

    private func segmentHalf(_ title: String, isOn: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title)
                .scaledFont(size: 15, weight: .semibold)
                .foregroundStyle(isOn ? .white : Color.inkSecondary)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 11)
                .background(isOn ? Color.accent : .clear, in: Capsule())
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Общее

private func welcomeEyebrow(_ text: String) -> some View {
    Text(text)
        .font(.system(size: 12, weight: .semibold, design: .monospaced))
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
