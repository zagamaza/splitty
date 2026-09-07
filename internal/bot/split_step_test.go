package bot

import (
	"context"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Бот делит расход ТЕМ ЖЕ шагом, что и приложение.
//
// Раньше он писал доли как float64(sum)/n — те самые 33,333… — без копеечных
// полей, и в одной тусе расход из приложения делился до копейки, а из бота по
// рублю. Это первое условие включения признака дробного ввода
// (docs/plans/20260905-fractional-amounts.md).
func TestBotSplitsWithRoomStep(t *testing.T) {
	members := []api.User{{ID: 1}, {ID: 2}, {ID: 3}}

	cases := []struct {
		name       string
		fractional bool
		want       []int64
	}{
		{"туса без копеек — целый шаг", false, []int64{3400, 3300, 3300}},
		{"туса с копейками — копеечный шаг", true, []int64{3334, 3333, 3333}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fractional := tc.fractional
			room := &api.Room{
				ID: primitive.NewObjectID(), Name: "Туса", Currency: "RUB",
				Members: &members, FractionalAmounts: &fractional,
			}
			os := &recordingOperationService{}
			screen := NewAddDonorOperation(noopChatStateService{}, noopButtonService{}, os,
				&recordingRoomService{room: room}, noopRoomStateService{}, &Config{})

			upd := canonicalUpdate(addDonorOperation, &api.CallbackData{RoomId: room.ID.Hex()})
			// Page несёт сумму расхода, ExternalData — описание: так их кладёт
			// предыдущий экран мастера.
			upd.ChatState = &api.ChatState{CallbackData: &api.CallbackData{
				RoomId: room.ID.Hex(), Page: 100, ExternalData: "Ужин",
			}}
			upd.Button = &api.Button{CallbackData: &api.CallbackData{ExternalData: string(equally)}}

			screen.OnMessage(context.Background(), upd)

			if os.created == nil {
				t.Fatal("расход не создан")
			}
			if os.created.SumMinor == nil || *os.created.SumMinor != 10000 {
				t.Fatalf("итог в копейках = %v, want 10000", os.created.SumMinor)
			}
			if len(os.created.RecipientsWithSum) != len(tc.want) {
				t.Fatalf("получателей %d, want %d", len(os.created.RecipientsWithSum), len(tc.want))
			}
			var sum int64
			for i, r := range os.created.RecipientsWithSum {
				if r.SumMinor == nil {
					t.Fatalf("доля[%d] без копеек — расход из бота останется легаси-формой", i)
				}
				if *r.SumMinor != tc.want[i] {
					t.Errorf("доля[%d] = %d, want %d", i, *r.SumMinor, tc.want[i])
				}
				sum += *r.SumMinor
			}
			if sum != 10000 {
				t.Errorf("сумма долей = %d, want 10000", sum)
			}
		})
	}
}
