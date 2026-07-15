import PhotosUI
import SwiftUI
import UIKit

/// Съёмка/выбор фото чека для AI-распознавания. Выдаёт JPEG (`image/jpeg`),
/// уменьшенный до ~1024px по большей стороне — держит размер части под
/// серверным лимитом (image ≤8 МБ) и не грузит лишние мегапиксели в Gemini.
@MainActor
@Observable
final class ReceiptCapture {
    /// JPEG последнего снятого/выбранного чека (mime `image/jpeg`) — его шлёт
    /// `APIClient.parseOperation(image:)`. nil до выбора/после сброса.
    private(set) var imageData: Data?

    /// MIME результата — фиксированный `image/jpeg`.
    let mimeType = "image/jpeg"

    /// Максимальная сторона результата в пикселях.
    private let maxDimension: CGFloat = 1024
    /// Качество JPEG-сжатия.
    private let compressionQuality: CGFloat = 0.7

    /// Обрабатывает выбор из `PhotosPicker`: грузит данные, ужимает до JPEG.
    /// Возвращает true при успехе. Вызывать при смене `PhotosPickerItem`.
    @discardableResult
    func load(from item: PhotosPickerItem?) async -> Bool {
        guard let item,
              let data = try? await item.loadTransferable(type: Data.self),
              let image = UIImage(data: data) else {
            return false
        }
        return setImage(image)
    }

    /// Принимает снимок с камеры (`UIImagePickerController`) и ужимает его.
    @discardableResult
    func setImage(_ image: UIImage) -> Bool {
        guard let jpeg = Self.downscaledJPEG(
            image, maxDimension: maxDimension, quality: compressionQuality
        ) else {
            return false
        }
        imageData = jpeg
        return true
    }

    /// Сбрасывает выбранный чек (после отправки/отмены).
    func reset() {
        imageData = nil
    }

    /// Уменьшает изображение до `maxDimension` по большей стороне (без апскейла)
    /// и кодирует в JPEG. nil — кодирование не удалось.
    static func downscaledJPEG(
        _ image: UIImage, maxDimension: CGFloat, quality: CGFloat
    ) -> Data? {
        let size = image.size
        let longest = max(size.width, size.height)
        guard longest > 0 else { return nil }
        let scale = longest > maxDimension ? maxDimension / longest : 1
        let target = CGSize(
            width: (size.width * scale).rounded(),
            height: (size.height * scale).rounded()
        )

        let format = UIGraphicsImageRendererFormat.default()
        format.scale = 1
        format.opaque = true
        let renderer = UIGraphicsImageRenderer(size: target, format: format)
        let resized = renderer.image { _ in
            image.draw(in: CGRect(origin: .zero, size: target))
        }
        return resized.jpegData(compressionQuality: quality)
    }
}

/// Камера для съёмки чека (`UIImagePickerController` — `PhotosPicker` камеру не
/// открывает). Результат отдаётся в `onCapture` как `UIImage`; ужатие в JPEG —
/// на стороне `ReceiptCapture.setImage`.
struct CameraPicker: UIViewControllerRepresentable {
    let onCapture: (UIImage) -> Void
    @Environment(\.dismiss) private var dismiss

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ uiViewController: UIImagePickerController, context: Context) {}

    func makeCoordinator() -> Coordinator {
        Coordinator(onCapture: onCapture, dismiss: { dismiss() })
    }

    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        private let onCapture: (UIImage) -> Void
        private let dismiss: () -> Void

        init(onCapture: @escaping (UIImage) -> Void, dismiss: @escaping () -> Void) {
            self.onCapture = onCapture
            self.dismiss = dismiss
        }

        func imagePickerController(
            _ picker: UIImagePickerController,
            didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]
        ) {
            if let image = info[.originalImage] as? UIImage {
                onCapture(image)
            }
            dismiss()
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            dismiss()
        }
    }
}
