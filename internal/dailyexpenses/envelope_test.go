package dailyexpenses

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// JSON выгрузки закреплён целиком: поля, их порядок и значения.
//
// Наружу это уходит на сторонний адрес, где никто не читает наш код. Случайная
// правка модели раньше молча меняла смысл отданного числа; теперь она роняет
// этот тест.
func TestExportEnvelopeJSON(t *testing.T) {
	roomID, _ := primitive.ObjectIDFromHex("65af00000000000000000001")
	opID, _ := primitive.ObjectIDFromHex("65af00000000000000000002")
	fractional := true
	minor := int64(2080)
	share := int64(1040)

	room := api.Room{
		ID: roomID, Name: "Бали", Currency: "USD", FractionalAmounts: &fractional,
	}
	op := api.Operation{
		ID: opID, Description: "Ужин", Sum: 21, SumMinor: &minor,
		Donor:    &api.User{ID: 10},
		Status:   "active",
		CreateAt: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
		RecipientsWithSum: []api.RecipientWithSum{
			{User: api.User{ID: 10}, Sum: 10.4, SumMinor: &share},
			{User: api.User{ID: 20}, Sum: 10.4, SumMinor: &share},
		},
	}

	envelope := exportEnvelope{
		Version:     exportVersion,
		GeneratedAt: time.Date(2026, 9, 9, 12, 30, 0, 0, time.UTC),
		Expenses:    exportExpenses(room, []api.Operation{op}),
	}
	got, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	want := `{"version":1,"generatedAt":"2026-09-09T12:30:00Z","expenses":[` +
		`{"id":"65af00000000000000000002","roomId":"65af00000000000000000001","roomName":"Бали",` +
		`"currency":"USD","exponent":2,"description":"Ужин","sum":21,"sumMinor":2080,` +
		`"donorId":10,"isDebtRepayment":false,"status":"active","createdAt":"2026-09-09T12:00:00Z",` +
		`"recipients":[{"userId":10,"sum":10,"sumMinor":1040},{"userId":20,"sum":10,"sumMinor":1040}]}]}`
	if string(got) != want {
		t.Errorf("конверт изменился:\n got: %s\nwant: %s", got, want)
	}
}

// У комнаты без валюты в конверте стоит умолчание, а не пустая строка:
// получателю нечем истолковать «».
func TestExportEnvelopeFillsDefaultCurrency(t *testing.T) {
	room := api.Room{ID: primitive.NewObjectID(), Name: "Стамбул"}
	op := api.Operation{ID: primitive.NewObjectID(), Sum: 100, Donor: &api.User{ID: 1}}

	expenses := exportExpenses(room, []api.Operation{op})
	if len(expenses) != 1 {
		t.Fatalf("расходов %d, want 1", len(expenses))
	}
	if expenses[0].Currency != api.DefaultCurrency {
		t.Errorf("валюта = %q, want %q", expenses[0].Currency, api.DefaultCurrency)
	}
	// Легаси-операция без копеечного поля отдаётся точной величиной из целого.
	if expenses[0].SumMinor != 10000 {
		t.Errorf("точная величина = %d, want 10000", expenses[0].SumMinor)
	}
}
