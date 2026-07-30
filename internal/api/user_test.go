package api

import (
	"reflect"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func intp(i int) *int { return &i }

func TestHasTelegram(t *testing.T) {
	tests := []struct {
		name string
		user *User
		want bool
	}{
		{"nil-получатель", nil, false},
		{"telegram_id не задан", &User{ID: 1}, false},
		{"telegram_id ноль", &User{ID: 1, TelegramID: intp(0)}, false},
		{"telegram_id задан", &User{ID: 1, TelegramID: intp(123456789)}, true},
		{"синтетический _id с telegram", &User{ID: 1_000_000_000_001, TelegramID: intp(42)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.HasTelegram(); got != tt.want {
				t.Fatalf("HasTelegram() = %v, ожидалось %v", got, tt.want)
			}
		})
	}
}

func TestIsDeleted(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		user *User
		want bool
	}{
		{"nil-получатель", nil, false},
		{"живой", &User{ID: 1}, false},
		{"tombstone", &User{ID: 1, DeletedAt: &now}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.IsDeleted(); got != tt.want {
				t.Fatalf("IsDeleted() = %v, ожидалось %v", got, tt.want)
			}
		})
	}
}

func TestSnapshotClearsIdentity(t *testing.T) {
	now := time.Now()
	u := User{
		ID:                777,
		Username:          "vasya",
		DisplayName:       "Вася Пупкин",
		UserLang:          "ru",
		SelectedLang:      "en",
		NotificationOn:    boolp(true),
		CountInPage:       10,
		BankDetails:       "СБП +7999...",
		Notify:            &NotifySettings{Operations: ChannelPrefs{Push: boolp(false)}},
		Aliases:           []string{"Саня"},
		PushTokens:        []PushToken{{Token: "fcm-token", Platform: "ios"}},
		TelegramID:        intp(123456789),
		GoogleSub:         "google-sub",
		AppleSub:          "apple-sub",
		Email:             "vasya@example.com",
		DeletedAt:         &now,
		AppleRefreshToken: "apple-refresh",
	}

	s := u.Snapshot()

	if s.ID != 777 || s.Username != "vasya" || s.DisplayName != "Вася Пупкин" || s.UserLang != "ru" {
		t.Fatalf("Snapshot потерял отображаемые поля: %+v", s)
	}
	if s.TelegramID != nil || s.GoogleSub != "" || s.AppleSub != "" || s.Email != "" ||
		s.AppleRefreshToken != "" || s.DeletedAt != nil {
		t.Fatalf("Snapshot оставил поля личности: %+v", s)
	}
	if s.PushTokens != nil || s.Notify != nil || s.Aliases != nil || s.BankDetails != "" {
		t.Fatalf("Snapshot оставил персональные данные: %+v", s)
	}

	// оригинал не мутирован — Snapshot работает на копии
	if u.TelegramID == nil || u.GoogleSub == "" || u.PushTokens == nil {
		t.Fatalf("Snapshot мутировал оригинал: %+v", u)
	}
}

// Тест-страж: любое поле User, не перечисленное в snapshotKeepsFields, обязано
// обнуляться в Snapshot. Новое поле, добавленное позже, автоматически попадает
// в «должно обнуляться» и уронит тест, если его забыли учесть, — так поля
// личности не утекут во встроенные снимки комнат молча.
func TestSnapshotGuardAllFields(t *testing.T) {
	snapshotKeepsFields := map[string]bool{
		"ID":             true,
		"Username":       true,
		"DisplayName":    true,
		"UserLang":       true,
		"SelectedLang":   true,
		"NotificationOn": true,
		"CountInPage":    true,
	}

	filled := reflect.New(reflect.TypeOf(User{})).Elem()
	for i := 0; i < filled.NumField(); i++ {
		f := filled.Type().Field(i)
		if err := fillNonZero(filled.Field(i)); err != nil {
			t.Fatalf("поле %s: %v (добавьте поддержку типа в fillNonZero)", f.Name, err)
		}
		if filled.Field(i).IsZero() {
			t.Fatalf("поле %s не удалось заполнить ненулевым значением", f.Name)
		}
	}

	orig := filled.Interface().(User)
	snap := reflect.ValueOf(orig.Snapshot())

	for i := 0; i < snap.NumField(); i++ {
		name := snap.Type().Field(i).Name
		if snapshotKeepsFields[name] {
			if snap.Field(i).IsZero() {
				t.Errorf("поле %s должно сохраняться в Snapshot, но обнулено", name)
			}
			continue
		}
		if !snap.Field(i).IsZero() {
			t.Errorf("поле %s не обнулено в Snapshot: если это не PII и не поле личности — "+
				"добавьте его в snapshotKeepsFields осознанно", name)
		}
	}
}

// fillNonZero кладёт в поле ненулевое значение. Незнакомый тип — ошибка, чтобы
// новое поле User нельзя было добавить, не подумав про Snapshot.
func fillNonZero(v reflect.Value) error {
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(42)
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Ptr:
		v.Set(reflect.New(v.Type().Elem()))
		// для указателей на скаляры дополнительно кладём ненулевое значение,
		// чтобы HasTelegram-подобные проверки видели осмысленные данные
		switch v.Elem().Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.String, reflect.Bool:
			return fillNonZero(v.Elem())
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if err := fillNonZero(v.Field(i)); err != nil {
				return err
			}
		}
	default:
		return errUnsupportedKind{v.Kind()}
	}
	return nil
}

type errUnsupportedKind struct{ k reflect.Kind }

func (e errUnsupportedKind) Error() string {
	return "неподдерживаемый вид поля: " + e.k.String()
}

// Новые поля с omitempty не должны появляться в bson-документе пустыми:
// иначе sparse unique-индексы по telegram_id/google_sub/apple_sub увидели бы
// null у всех старых пользователей и отвергли бы второго такого же.
func TestUserBsonOmitsEmptyIdentityFields(t *testing.T) {
	raw, err := bson.Marshal(User{ID: 5, Username: "u", DisplayName: "d"})
	if err != nil {
		t.Fatalf("bson.Marshal: %v", err)
	}
	var doc bson.M
	if err := bson.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("bson.Unmarshal: %v", err)
	}
	for _, key := range []string{"telegram_id", "google_sub", "apple_sub", "email", "deleted_at", "apple_refresh_token"} {
		if _, ok := doc[key]; ok {
			t.Errorf("пустое поле %s попало в документ: %v", key, doc)
		}
	}
	if doc["_id"] != int32(5) && doc["_id"] != int64(5) {
		t.Errorf("_id должен сохраняться: %v", doc["_id"])
	}

	tg := 123
	raw, err = bson.Marshal(User{ID: 5, TelegramID: &tg, GoogleSub: "gs", Email: "e@x.y"})
	if err != nil {
		t.Fatalf("bson.Marshal: %v", err)
	}
	doc = bson.M{}
	if err := bson.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("bson.Unmarshal: %v", err)
	}
	for _, key := range []string{"telegram_id", "google_sub", "email"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("заполненное поле %s должно записываться: %v", key, doc)
		}
	}
}
