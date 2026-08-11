package api

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// InviteStatus текущее состояние отношения «пользователь × комната».
type InviteStatus string

const (
	// InviteAdded участника добавил другой участник (основной путь): человек уже
	// в комнате, карточка в разделе информационная.
	InviteAdded InviteStatus = "added"
	// InviteLeft человек вышел из комнаты сам или был удалён. Тихо добавить его
	// обратно нельзя — иначе получался бы цикл «убрал себя из расхода → вышел →
	// добавили снова».
	InviteLeft InviteStatus = "left"
	// InvitePending повторное приглашение после выхода: участником НЕ делает,
	// ждёт явного решения человека.
	InvitePending InviteStatus = "pending"
	// InviteDeclined человек отказался возвращаться.
	InviteDeclined InviteStatus = "declined"
)

// RoomInvite — ОДНА запись на пару (комната, приглашённый), хранящая текущее
// состояние отношения, а не историю приглашений. Уникальный индекс по паре
// гарантирует единственность, Upsert обновляет запись на месте.
//
// Поля SeenAt здесь намеренно нет: прочитанность решена одной отметкой на
// пользователе (User.NotificationsSeenAt) — второй механизм рядом противоречил
// бы принятой модели и остался бы мёртвым.
type RoomInvite struct {
	ID        primitive.ObjectID `json:"id" bson:"_id,omitempty"`
	RoomID    primitive.ObjectID `json:"roomId" bson:"room_id"`
	InviteeID int                `json:"inviteeId" bson:"invitee_id"`
	InviterID int                `json:"inviterId" bson:"inviter_id"`
	Status    InviteStatus       `json:"status" bson:"status"`
	// CreatedAt время ПОСЛЕДНЕЙ смены отношения, а не первого приглашения:
	// каждое новое приглашение — новое событие для раздела уведомлений. Иначе
	// после accept повторного приглашения время осталось бы от pending,
	// оказалось бы старше отметки прочитанного, и карточка не показалась бы.
	CreatedAt time.Time `json:"createdAt" bson:"created_at"`
	// Version номер версии записи, растёт на каждую запись ($inc). Нужен
	// условной записи (UpsertIfUnchanged) как токен compare-and-set. Раньше эту
	// роль играл created_at — и играл неверно: mongo хранит даты с точностью до
	// миллисекунды, поэтому чужая запись, легшая в ту же миллисекунду, оставляла
	// фильтр совпадающим и протухшее решение затирало более свежее. У записей,
	// созданных до появления поля, версия читается как 0 — см. versionFilter.
	Version int `json:"-" bson:"version"`
}
