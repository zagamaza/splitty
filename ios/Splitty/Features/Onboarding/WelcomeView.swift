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
                    .scaledFont(size: 15, weight: .medium)
                    .foregroundStyle(Color.inkSecondary)
                    .accessibilityIdentifier("welcomeSkip")
            }
            .padding(.horizontal, 16)
            .padding(.top, 8)

            TabView(selection: $page) {
                page1.tag(0)
                page2.tag(1)
                page3.tag(2)
                page4.tag(3)
            }
            .tabViewStyle(.page(indexDisplayMode: .never))

            PageDots(count: Self.pageCount, current: page)
                .padding(.bottom, 18)

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

    // MARK: Экраны

    private var page1: some View {
        WelcomePage(
            title: "Группа — это общий счёт",
            subtitle: "Поездка, съёмная квартира или один ужин. Платит кто-то один, а расход делят все."
        ) {
            SharedBillArt()
        }
    }

    private var page2: some View {
        WelcomePage(
            title: "Скажите — запишем",
            subtitle: "Продиктуйте расход вслух, и он появится в группе. Можно снять чек или вписать руками."
        ) {
            DictationArt()
        }
    }

    private var page3: some View {
        WelcomePage(
            title: "Кто сколько заплатил",
            subtitle: "Splitty делит каждый расход на участников. Ужин 600 на троих — по 200, такси 300 — по 100."
        ) {
            WhoPaidArt()
        }
    }

    private var page4: some View {
        WelcomePage(
            title: "Платите один раз",
            subtitle: "Ваша доля 300 ₽ уходит одним переводом, а не двумя: Splitty сводит долги внутри группы."
        ) {
            TransfersArt()
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
            art()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.accent.opacity(0.07), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
                .padding(.horizontal, 16)

            VStack(spacing: 8) {
                Text(title)
                    .scaledFont(size: 21, weight: .bold, relativeTo: .title3)
                    .foregroundStyle(Color.ink)
                    .multilineTextAlignment(.center)
                Text(subtitle)
                    .scaledFont(size: 14, relativeTo: .subheadline)
                    .foregroundStyle(Color.inkSecondary)
                    .multilineTextAlignment(.center)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(.horizontal, 24)
            .padding(.top, 20)
            .padding(.bottom, 8)
        }
    }
}

private struct PageDots: View {
    let count: Int
    let current: Int

    var body: some View {
        HStack(spacing: 6) {
            ForEach(0..<count, id: \.self) { index in
                Capsule()
                    .fill(index == current ? Color.accent : Color.hairline)
                    .frame(width: index == current ? 18 : 6, height: 6)
                    .animation(.easeInOut(duration: 0.2), value: current)
            }
        }
        .accessibilityHidden(true)
    }
}

// MARK: - Иллюстрации

/// Расходы падают в общий счёт группы.
private struct SharedBillArt: View {
    @State private var visible = 0
    private let slips = [("Ужин", "600 ₽"), ("Такси", "300 ₽"), ("Продукты", "450 ₽")]

    var body: some View {
        VStack(spacing: 10) {
            ForEach(Array(slips.enumerated()), id: \.offset) { index, slip in
                HStack {
                    Text(slip.0).scaledFont(size: 12, weight: .semibold)
                    Spacer()
                    Text(slip.1).font(.system(size: 12, weight: .semibold, design: .monospaced))
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 9)
                .background(Color.surface, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
                .shadow(color: .black.opacity(0.06), radius: 6, y: 2)
                .opacity(index < visible ? 1 : 0)
                .offset(y: index < visible ? 0 : -10)
            }

            Text("Общий счёт группы")
                .scaledFont(size: 12, weight: .semibold)
                .foregroundStyle(Color.accentText)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 13)
                .overlay(
                    RoundedRectangle(cornerRadius: 13, style: .continuous)
                        .strokeBorder(Color.accent.opacity(0.55), style: StrokeStyle(lineWidth: 1.5, dash: [5, 4]))
                )
                .padding(.top, 4)
        }
        .padding(18)
        .task {
            // Появление по одной: три расхода складываются в один счёт.
            for index in 1...slips.count {
                try? await Task.sleep(nanoseconds: 220_000_000)
                withAnimation(.easeOut(duration: 0.3)) { visible = index }
            }
        }
    }
}

/// Запись голоса и мини-чек, который из неё получается.
private struct DictationArt: View {
    @State private var words = 0
    @State private var showReceipt = false

    private let phrase = ["пицца", "за", "восемьсот", "и", "кола", "за", "двести", "пополам", "с", "Саней"]

    var body: some View {
        ZStack {
            if showReceipt {
                MiniReceipt()
                    .padding(.horizontal, 26)
                    .transition(.scale(scale: 0.94).combined(with: .opacity))
            } else {
                recording
                    .transition(.opacity)
            }
        }
        .task { await play() }
    }

    private var recording: some View {
        VStack(spacing: 10) {
            Text(phrase.prefix(words).joined(separator: " "))
                .scaledFont(size: 13, weight: .bold)
                .foregroundStyle(.white)
                .multilineTextAlignment(.center)
                .frame(height: 54, alignment: .bottom)
                .padding(.horizontal, 14)

            WelcomeWaveform()

            HStack(spacing: 5) {
                Circle().fill(Color.negative).frame(width: 5, height: 5)
                Text("0:06").font(.system(size: 11, weight: .semibold, design: .monospaced))
                    .foregroundStyle(.white.opacity(0.9))
            }

            ZStack {
                Circle()
                    .fill(Color.accent)
                    .frame(width: 52, height: 52)
                Image(systemName: "mic.fill")
                    .font(.system(size: 20, weight: .semibold))
                    .foregroundStyle(.white)
            }

            Text("Отпустите — распознать · вверх — закрепить")
                .scaledFont(size: 9)
                .foregroundStyle(.white.opacity(0.5))
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(light: 0x2B313A, dark: 0x05080B))
    }

    private func play() async {
        while !Task.isCancelled {
            withAnimation { showReceipt = false }
            words = 0
            for index in 1...phrase.count {
                try? await Task.sleep(nanoseconds: 220_000_000)
                if Task.isCancelled { return }
                withAnimation(.easeOut(duration: 0.2)) { words = index }
            }
            try? await Task.sleep(nanoseconds: 700_000_000)
            if Task.isCancelled { return }
            withAnimation(.easeOut(duration: 0.35)) { showReceipt = true }
            try? await Task.sleep(nanoseconds: 2_400_000_000)
        }
    }
}

private struct WelcomeWaveform: View {
    @State private var phase: CGFloat = 0

    var body: some View {
        HStack(spacing: 3) {
            ForEach(0..<11, id: \.self) { index in
                Capsule()
                    .fill(Color.white.opacity(0.92))
                    .frame(width: 2.5, height: height(for: index))
            }
        }
        .frame(height: 24)
        .task {
            // Волна живая, но нарочно неспешная: экран объясняет, а не пляшет.
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: 120_000_000)
                withAnimation(.easeInOut(duration: 0.12)) { phase += 1 }
            }
        }
    }

    private func height(for index: Int) -> CGFloat {
        let base = [6.0, 14.0, 20.0, 11.0, 17.0, 22.0, 9.0, 15.0, 19.0, 12.0, 7.0]
        let shifted = (index + Int(phase)) % base.count
        return base[shifted]
    }
}

/// Мини-чек: уменьшенный `ReceiptCard` с экрана расхода.
private struct MiniReceipt: View {
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("позиции").font(.system(size: 9, design: .monospaced))
                Spacer()
                Text("2 поз.").font(.system(size: 9, design: .monospaced))
            }
            .foregroundStyle(Color.inkSecondary)

            dashed
            item(name: "Пицца", sum: "800 ₽", each: "по 400 ₽ × 2")
            dashed
            item(name: "Кола", sum: "200 ₽", each: "по 100 ₽ × 2")

            Rectangle().fill(Color.ink).frame(height: 1.5).padding(.vertical, 7)

            HStack {
                Text("Итого").scaledFont(size: 12, weight: .bold)
                Spacer()
                Text("1000 ₽").font(.system(size: 13, weight: .bold, design: .monospaced))
            }
        }
        .padding(12)
        .background(Color.receiptPaper)
        .clipShape(RoundedRectangle(cornerRadius: 3, style: .continuous))
        .shadow(color: .black.opacity(0.18), radius: 12, y: 6)
    }

    private var dashed: some View {
        Rectangle()
            .fill(Color.hairline)
            .frame(height: 1)
            .padding(.vertical, 7)
    }

    private func item(name: String, sum: String, each: String) -> some View {
        VStack(spacing: 5) {
            HStack {
                Text(name).scaledFont(size: 11, weight: .bold)
                Spacer()
                Text(sum).font(.system(size: 11, weight: .bold, design: .monospaced))
            }
            HStack {
                HStack(spacing: -4) {
                    avatar("Я", color: .accent)
                    avatar("С", color: Color(red: 0.55, green: 0.36, blue: 0.96))
                }
                Spacer()
                Text(each).font(.system(size: 9, design: .monospaced)).foregroundStyle(Color.inkSecondary)
            }
        }
    }

    private func avatar(_ letter: String, color: Color) -> some View {
        Text(letter)
            .font(.system(size: 7, weight: .bold))
            .foregroundStyle(.white)
            .frame(width: 14, height: 14)
            .background(color, in: Circle())
    }
}

/// Кто сколько заплатил — предыстория для последнего экрана.
private struct WhoPaidArt: View {
    var body: some View {
        VStack(spacing: 10) {
            paidCard(initial: "А", name: "Аня заплатила", what: "ужин", sum: "600 ₽", share: "по 200 ₽", color: .accent)
            paidCard(initial: "Б", name: "Боря заплатил", what: "такси", sum: "300 ₽", share: "по 100 ₽",
                     color: Color(red: 0.18, green: 0.43, blue: 0.89))

            Spacer(minLength: 0)

            HStack(spacing: 9) {
                Text("Я")
                    .font(.system(size: 11, weight: .bold))
                    .foregroundStyle(.white)
                    .frame(width: 26, height: 26)
                    .background(Color.inkSecondary, in: Circle())
                VStack(alignment: .leading, spacing: 2) {
                    Text("Вы не платили ничего").scaledFont(size: 11.5, weight: .semibold)
                    Text("значит, ваша доля — за вами")
                        .scaledFont(size: 10)
                        .foregroundStyle(Color.inkSecondary)
                }
                Spacer(minLength: 0)
                Text("300 ₽")
                    .font(.system(size: 14, weight: .bold, design: .monospaced))
                    .foregroundStyle(Color.accentText)
            }
            .padding(11)
            .background(Color.accent.opacity(0.1), in: RoundedRectangle(cornerRadius: 13, style: .continuous))
        }
        .padding(14)
    }

    private func paidCard(initial: String, name: String, what: String, sum: String, share: String, color: Color) -> some View {
        VStack(spacing: 9) {
            HStack(spacing: 8) {
                Text(initial)
                    .font(.system(size: 11, weight: .bold))
                    .foregroundStyle(.white)
                    .frame(width: 26, height: 26)
                    .background(color, in: Circle())
                VStack(alignment: .leading, spacing: 1) {
                    Text(name).scaledFont(size: 12, weight: .semibold)
                    Text(what).scaledFont(size: 9.5).foregroundStyle(Color.inkSecondary)
                }
                Spacer(minLength: 0)
                Text(sum).font(.system(size: 14, weight: .bold, design: .monospaced))
            }

            HStack(spacing: 6) {
                Text("делим на троих").scaledFont(size: 10).foregroundStyle(Color.inkSecondary)
                Text(share)
                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                    .foregroundStyle(Color.accentText)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 3)
                    .background(Color.accent.opacity(0.13), in: Capsule())
                Spacer(minLength: 0)
            }
            .padding(.top, 9)
            .overlay(alignment: .top) {
                Rectangle().fill(Color.hairline).frame(height: 1)
            }
        }
        .padding(11)
        .background(Color.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .shadow(color: .black.opacity(0.06), radius: 6, y: 2)
    }
}

/// Сколько раз платить: сравнение «Без Splitty / Со Splitty» переключателем.
///
/// Темп задаёт человек: автоматическая смена состояний читалась как мельтешение,
/// а понять надо ровно одно — сколько строк осталось.
private struct TransfersArt: View {
    @State private var withSplitty = false

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            Text("Вам переводить")
                .font(.system(size: 9.5, weight: .semibold, design: .monospaced))
                .foregroundStyle(Color.inkSecondary)
                .textCase(.uppercase)

            payRow(initial: "А", name: "Ане", sum: withSplitty ? "300 ₽" : "200 ₽", color: .accent)

            if !withSplitty {
                payRow(initial: "Б", name: "Боре", sum: "100 ₽",
                       color: Color(red: 0.18, green: 0.43, blue: 0.89))
                    .transition(.move(edge: .top).combined(with: .opacity))
            }

            if withSplitty {
                HStack(alignment: .top, spacing: 7) {
                    Image(systemName: "checkmark")
                        .font(.system(size: 10, weight: .bold))
                    Text("Боря заплатил ровно свою долю — с ним вы в расчёте. Его 100 ₽ уходят Ане вместе с вашими.")
                        .scaledFont(size: 10.5)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .foregroundStyle(Color.accentText)
                .padding(10)
                .background(Color.accent.opacity(0.11), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .transition(.opacity)
            }

            Spacer(minLength: 0)

            HStack(spacing: 7) {
                Spacer(minLength: 0)
                ForEach(0..<2, id: \.self) { index in
                    RoundedRectangle(cornerRadius: 2)
                        .fill(mark(at: index))
                        .frame(width: 9, height: 12)
                }
                Text(withSplitty ? "1 перевод" : "2 перевода")
                    .scaledFont(size: 11, weight: .bold)
                    .foregroundStyle(withSplitty ? Color.accentText : Color.negative)
                Spacer(minLength: 0)
            }

            Picker("", selection: $withSplitty) {
                Text("Без Splitty").tag(false)
                Text("Со Splitty").tag(true)
            }
            .pickerStyle(.segmented)
            .accessibilityIdentifier("welcomeCompare")
        }
        .padding(13)
        .animation(.easeInOut(duration: 0.35), value: withSplitty)
    }

    private func mark(at index: Int) -> Color {
        if withSplitty {
            return index == 0 ? Color.accent : Color.hairline
        }
        return Color.negative
    }

    private func payRow(initial: String, name: String, sum: String, color: Color) -> some View {
        HStack(spacing: 9) {
            Text(initial)
                .font(.system(size: 11, weight: .bold))
                .foregroundStyle(.white)
                .frame(width: 28, height: 28)
                .background(color, in: Circle())
            Text(name).scaledFont(size: 13, weight: .semibold)
            Spacer(minLength: 0)
            Text(sum)
                .font(.system(size: 14, weight: .bold, design: .monospaced))
                .foregroundStyle(withSplitty ? Color.accentText : Color.negative)
        }
        .padding(.horizontal, 11)
        .frame(height: 46)
        .background(Color.surface, in: RoundedRectangle(cornerRadius: 13, style: .continuous))
        .shadow(color: .black.opacity(0.06), radius: 6, y: 2)
    }
}
