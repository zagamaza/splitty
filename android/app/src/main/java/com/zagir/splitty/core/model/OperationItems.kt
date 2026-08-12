package com.zagir.splitty.core.model

/**
 * Логика деления itemized-чека поверх транспортных DTO [OperationItem]/[ItemShare]
 * (Task 1 — только декод/энкод). Клиентское превью «кто сколько должен» — ТОЧНОЕ
 * зеркало серверного `DeriveShares` (`internal/api/itemsplit.go`): снять фиксы →
 * остаток по весам с детерминированным tie-break по userId → надбавки на базу.
 *
 * Зачем зеркало, а не запрос к серверу: форма показывает суммы ДО сохранения,
 * и они обязаны совпасть с теми, что сервер запишет в плоские recipients — иначе
 * пользователь увидит одно, а в группе появится другое.
 *
 * Про переполнение: цены и суммы 64-битные, как на сервере, поэтому сужения на
 * выходе больше нет. Произведение price*weight по-прежнему может переполнить
 * Long — на этот случай во внутренних функциях осталась защита, отдающая null
 * вместо завёрнутого числа.
 */

/** true — надбавка (сбор/чаевые/доставка), делится по базе, а не по своим долям. */
val OperationItem.isSurcharge: Boolean get() = kind == OperationItem.KIND_SURCHARGE

/** Доли без опциональности (надбавки приходят без `shares`). */
val OperationItem.shareList: List<ItemShare> get() = shares ?: emptyList()

/**
 * true — есть нераспознанные имена: черновик нельзя сохранять (сервер вернёт 400),
 * пользователь должен сопоставить имена участникам.
 */
val OperationItem.hasUnknown: Boolean get() = !unknown.isNullOrEmpty()

/** Результат превью долей: карта userId→сумма и общий итог (обе — целые единицы). */
data class DerivedShares(val shares: Map<Long, Long>, val total: Long)

/**
 * Строка разбивки «С кого сколько» itemized-черновика: итог участника и сколько
 * из него добавили сборы (для подписи «+N ₽ сбор»). Порт iOS `PersonShare`.
 */
data class PersonShare(
    val userId: Long,
    /** Полная доля: позиции + сборы (ровно то, что сохранит сервер). */
    val total: Long,
    /** Часть итога, пришедшая от сборов/чаевых; 0 — сборов нет. */
    val surchargePart: Long,
)

/**
 * Клиентское превью «кто сколько должен» по позициям — зеркало серверного
 * `DeriveShares`. Обычные позиции считаются первыми (образуют базу), затем на неё
 * накладываются надбавки. Возвращает null, если позиции невалидны (перебор фиксов,
 * неразделённый остаток, надбавка без цены, нарушенный инвариант «сумма == итог»).
 */
fun List<OperationItem>.derivedShares(): DerivedShares? {
    val base = LinkedHashMap<Long, Long>()
    var total = 0L
    for (item in this) {
        if (item.isSurcharge) continue
        val split = splitItem(item.price, item.shareList) ?: return null
        for ((id, value) in split) base[id] = (base[id] ?: 0L) + value
        total += item.price
    }

    val out = LinkedHashMap<Long, Long>(base)
    for (item in this) {
        if (!item.isSurcharge) continue
        if (item.price <= 0) return null
        val surcharge = splitSurcharge(item.price, item.split, base) ?: return null
        for ((id, value) in surcharge) out[id] = (out[id] ?: 0L) + value
        total += item.price
    }

    var sum = 0L
    for (value in out.values) sum += value
    if (sum != total) return null

    return DerivedShares(HashMap(out), total)
}

/**
 * userId'ы участников из ОБЫЧНЫХ позиций в стабильном порядке появления
 * (надбавки делятся по базе, их доли сюда не входят). Порт iOS `itemizedUserIds`.
 */
fun List<OperationItem>.itemizedUserIds(): List<Long> {
    val ordered = LinkedHashSet<Long>()
    for (item in this) {
        if (item.isSurcharge) continue
        for (share in item.shareList) ordered.add(share.userId)
    }
    return ordered.toList()
}

/**
 * Разбивка «С кого сколько» по позициям (порт iOS `personShares`): обычные позиции
 * образуют базу, надбавки накладываются сверху; порядок — появление участников в
 * чеке. null — позиций нет или доли невыводимы (перебор фиксов и т.п.).
 */
fun List<OperationItem>.personShares(): List<PersonShare>? {
    if (isEmpty()) return null
    val derived = derivedShares() ?: return null
    val base = this.filter { !it.isSurcharge }.derivedShares()?.shares ?: emptyMap()
    return itemizedUserIds().mapNotNull { id ->
        val total = derived.shares[id] ?: return@mapNotNull null
        PersonShare(userId = id, total = total, surchargePart = total - (base[id] ?: 0L))
    }
}

/** Участник и его вес для целочисленного взвешенного деления. */
internal data class WeightShare(val id: Long, val weight: Long)

/**
 * Делит `amount` между участниками пропорционально весам. Базовая доля —
 * floor(amount*weight/totalWeight); остаток от округления раздаётся по одному тем,
 * у кого базовая доля больше (при равенстве — меньший userId). Сумма долей всегда
 * равна `amount`. null — переполнение/некорректный вход (зеркало `splitByWeight`).
 */
internal fun splitByWeight(amount: Long, weights: List<WeightShare>): Map<Long, Long>? {
    if (amount < 0) return null
    var totalW = 0L
    for (w in weights) {
        if (w.weight < 0) return null
        if (totalW > Long.MAX_VALUE - w.weight) return null // аддитивное переполнение суммы весов
        totalW += w.weight
    }
    val out = LinkedHashMap<Long, Long>(weights.size)
    if (totalW <= 0) return out

    // Схлопываем дубли по id и отбрасываем нулевые веса ДО деления (зеркало
    // серверного splitByWeight): один участник может встретиться в shares дважды,
    // а у proportional-надбавки вес равен базовой доле и часто равен нулю.
    // Иначе остаток от округления уходил тому, кто ничего не ел, и дважды — дублю.
    val agg = LinkedHashMap<Long, Long>(weights.size)
    for (w in weights) {
        if (w.weight == 0L) continue
        val prev = agg[w.id] ?: 0L
        if (prev > Long.MAX_VALUE - w.weight) return null
        agg[w.id] = prev + w.weight
    }
    val shares = agg.map { WeightShare(it.key, it.value) }

    var given = 0L
    for (w in shares) {
        if (w.weight != 0L && amount > Long.MAX_VALUE / w.weight) return null // amount*weight переполняет Long
        val value = amount * w.weight / totalW
        out[w.id] = value
        given += value
    }
    val rem = amount - given
    // при корректных входах остаток строго меньше числа участников; иначе — признак
    // переполнения/бага: не раздаём в цикле (DoS), а сигналим null
    if (rem < 0 || rem > shares.size) return null
    if (rem == 0L) return out
    if (shares.isEmpty()) return out

    val order = shares.sortedWith(
        compareByDescending<WeightShare> { out[it.id] ?: 0L }.thenBy { it.id }
    )
    var i = 0
    while (i < rem) {
        val id = order[(i % order.size)].id
        out[id] = (out[id] ?: 0L) + 1L
        i++
    }
    return out
}

/**
 * Делит цену позиции: сначала снимаются фиксированные `amount`, остаток делится по
 * весам ([splitByWeight]). Сумма долей всегда равна `price`. null — перебор фиксов
 * над ценой, отрицательный фикс или неразделённый остаток (зеркало `SplitItem`).
 */
internal fun splitItem(price: Long, shares: List<ItemShare>): Map<Long, Long>? {
    if (price < 0) return null
    val out = LinkedHashMap<Long, Long>(shares.size)
    var fixed = 0L
    val weighted = ArrayList<WeightShare>()
    for (share in shares) {
        val amount = share.amount
        if (amount != null) {
            if (amount < 0) return null
            if (fixed > Long.MAX_VALUE - amount) return null // аддитивное переполнение суммы фиксов
            out[share.userId] = (out[share.userId] ?: 0L) + amount
            fixed += amount
            continue
        }
        if (share.weight > 0) weighted.add(WeightShare(share.userId, share.weight.toLong()))
    }
    if (fixed > price) return null
    val rem = price - fixed
    if (weighted.isEmpty()) {
        return if (rem != 0L) null else out
    }
    val d = splitByWeight(rem, weighted) ?: return null
    for ((id, value) in d) out[id] = (out[id] ?: 0L) + value
    // страховка контракта: сумма долей обязана равняться цене (ловит любой дефект деления)
    var got = 0L
    for (value in out.values) got += value
    if (got != price) return null
    return out
}

/**
 * Делит надбавку (сбор/чаевые/доставку) по базовым долям людей. proportional →
 * вес участника равен его базовой доле; equally (или база нулевая) — всем поровну.
 * `base` — суммы, выведенные из обычных позиций (зеркало `SplitSurcharge`).
 */
internal fun splitSurcharge(price: Long, rule: String?, base: Map<Long, Long>): Map<Long, Long>? {
    val ids = base.keys.sorted()
    var totalBase = 0L
    for (id in ids) totalBase += base[id] ?: 0L
    val weights = ArrayList<WeightShare>(ids.size)
    for (id in ids) {
        // пропорционально работает только при положительной базе; иначе (все нули)
        // откатываемся к делению поровну, чтобы сбор не потерялся
        val weight = if (rule == OperationItem.SPLIT_PROPORTIONAL && totalBase > 0) base[id] ?: 0L else 1L
        weights.add(WeightShare(id, weight))
    }
    return splitByWeight(price, weights)
}
