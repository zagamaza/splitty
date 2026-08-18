package api

// Статусы операции. Живут здесь, а не отдельно в REST и в боте: правило «какая
// операция считается действующей» одинаково для обоих путей, а две копии
// констант уже расходились трактовкой архива и пустого легаси-статуса
const (
	StatusDraft   OperationStatus = "draft"
	StatusActive  OperationStatus = "active"
	StatusArchive OperationStatus = "archive"
)

// NormalizedOperation приводит операцию к модели develop, НЕ мутируя оригинал
// (работает с копией): легаси-операции эпохи master-2021 — без status и без
// recipients_with_sum — считаются активными, а их доли синтезируются канонически
// из легаси-поля recipients (поровну, остаток первым по порядку массива).
// Активная операция без получателей (битые данные) исключается как draft —
// иначе она валила бы весь расчёт долгов комнаты
func NormalizedOperation(o Operation) Operation {
	if o.Status == "" {
		o.Status = StatusActive
	}
	if len(o.RecipientsWithSum) == 0 && o.Recipients != nil && len(*o.Recipients) > 0 {
		recipients := *o.Recipients
		withSum := make([]RecipientWithSum, 0, len(recipients))
		for i := range recipients {
			withSum = append(withSum, RecipientWithSum{
				User: recipients[i],
				Sum:  float64(ShareOf(o.Sum, len(recipients), i)),
			})
		}
		o.RecipientsWithSum = withSum
	}
	if o.Status == StatusActive && len(o.RecipientsWithSum) == 0 {
		o.Status = StatusDraft
	}
	return o
}

// ActiveOperations возвращает нормализованные АКТИВНЫЕ операции комнаты.
// Работаем только с ними: драфты бота и архивные версии отредактированных
// операций не показываются и не участвуют в долгах/статистике (как в
// service.GetRoomDebts). База при нормализации не мутируется
func ActiveOperations(r *Room) []Operation {
	if r == nil || r.Operations == nil {
		return nil
	}
	var ops []Operation
	for _, o := range *r.Operations {
		n := NormalizedOperation(o)
		if n.Status == StatusActive {
			ops = append(ops, n)
		}
	}
	return ops
}

// HasOperations участвует ли пользователь хотя бы в одной АКТИВНОЙ операции —
// как донор или как получатель (доля роли не играет: нулевая доля тоже часть
// расчёта, и молчаливое исключение таких получателей разошлось бы с долгами).
//
// Единственное место правила «пока на человеке висят расходы — не выпускаем»:
// и REST, и бот спрашивают отсюда. Своя копия в боте уже расходилась дважды —
// легаси-получатель из recipients держал человека там, где REST его выпускал, а
// активная операция без долей запирала донора навсегда, без единого способа
// выбраться из телеграма
func HasOperations(r *Room, userId int) bool {
	for _, op := range ActiveOperations(r) {
		if op.Donor != nil && op.Donor.ID == userId {
			return true
		}
		for _, rec := range op.RecipientsWithSum {
			if rec.User.ID == userId {
				return true
			}
		}
	}
	return false
}

// RoomMembers nil-безопасно возвращает участников комнаты.
func RoomMembers(r *Room) []User {
	if r == nil || r.Members == nil {
		return nil
	}
	return *r.Members
}

// NormalizedRoom — копия комнаты с нормализованными АКТИВНЫМИ операциями: вход
// для расчёта долгов (service.GetRoomDebts).
//
// Живёт здесь, а не в rest: расчёт долгов зовёт не только REST, и на сырой
// комнате он врёт. Легаси-операции бота лежат с пустым status, и без
// нормализации ActiveOperations их отбрасывает — комната с долгами выглядела бы
// рассчитанной. Пустые срезы вместо nil — GetRoomDebts разыменовывает указатели
// без проверок.
func NormalizedRoom(r *Room) Room {
	ops := ActiveOperations(r)
	if ops == nil {
		ops = []Operation{}
	}
	members := RoomMembers(r)
	if members == nil {
		members = []User{}
	}
	if r == nil {
		return Room{Members: &members, Operations: &ops}
	}
	return Room{ID: r.ID, Name: r.Name, Members: &members, Operations: &ops}
}
