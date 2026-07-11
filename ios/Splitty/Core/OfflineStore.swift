import Foundation

/// Файловый JSON-кеш последних успешных ответов ключевых GET-эндпоинтов
/// (read-кеш офлайн-режима). Каталог — Application Support/SplittyCache,
/// файл на ключ (ключ = эндпоинт + параметры, см. `DataRepo`), запись
/// атомарная. Кеш — best effort: любые ошибки файловой системы/декодирования
/// молча дают промах (nil), приложение работает как без кеша.
/// Чистится при logout (вместе с outbox).
final class OfflineStore {
    private let directory: URL
    private let encoder: JSONEncoder
    private let decoder: JSONDecoder

    /// Каталог кеша по умолчанию: Application Support/SplittyCache.
    static var defaultDirectory: URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.temporaryDirectory
        return base.appendingPathComponent("SplittyCache", isDirectory: true)
    }

    /// `directory` переопределяется в тестах (временный каталог).
    init(directory: URL = OfflineStore.defaultDirectory) {
        self.directory = directory
        // Кодек симметричный (мы сами и пишем, и читаем): даты — ISO 8601.
        // Доли секунды серверных дат при перезаписи кеша теряются — для
        // отображения списков это несущественно.
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        self.encoder = encoder
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        self.decoder = decoder
    }

    /// Последний сохранённый ответ по ключу; nil — кеша нет или он битый.
    func read<T: Decodable>(key: String) -> T? {
        let url = fileURL(for: key)
        guard let data = try? Data(contentsOf: url) else { return nil }
        return try? decoder.decode(T.self, from: data)
    }

    /// Перезаписывает кеш ключа свежим ответом (атомарная запись файла).
    func write<T: Encodable>(_ value: T, key: String) {
        guard let data = try? encoder.encode(value) else { return }
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try? data.write(to: fileURL(for: key), options: [.atomic])
    }

    /// Полная очистка кеша (logout).
    func removeAll() {
        try? FileManager.default.removeItem(at: directory)
    }

    /// Имя файла для ключа: небезопасные для ФС символы заменяются на «_»
    /// (ключи контролируются кодом — только латиница/цифры/дефисы/hex id,
    /// коллизии исключены), расширение .json.
    static func fileName(for key: String) -> String {
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-._"))
        let sanitized = key.unicodeScalars
            .map { allowed.contains($0) ? Character($0) : "_" }
        return String(sanitized) + ".json"
    }

    private func fileURL(for key: String) -> URL {
        directory.appendingPathComponent(Self.fileName(for: key))
    }
}
