import SwiftUI
import XCTest
@testable import Splitty

/// Рендер экранов приветствия в PNG (`/tmp/splitty-snapshots/`) — визуальная
/// самопроверка: как это выглядит на самом деле, а не как задумывалось.
@MainActor
final class WelcomeRenderTests: XCTestCase {
    private let dir = URL(fileURLWithPath: "/tmp/splitty-snapshots", isDirectory: true)

    func testRenderEveryStep() throws {
        for step in WelcomeStep.allCases {
            try render(WelcomeStepView(step: step, isActive: false), name: "welcome-\(step.rawValue)", height: 780)
        }
    }

    private func render<V: View>(_ view: V, name: String, width: CGFloat = 390, height: CGFloat) throws {
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let content = view
            .frame(width: width, height: height)
            .background(Color.bg)
            .environment(SessionStore())
        let renderer = ImageRenderer(content: content)
        renderer.scale = 2
        renderer.proposedSize = ProposedViewSize(width: width, height: height)
        let image = try XCTUnwrap(renderer.uiImage, "рендер не удался: \(name)")
        try XCTUnwrap(image.pngData()).write(to: dir.appendingPathComponent("\(name).png"))
    }
}
