package service

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
)

// Сверка на копии живых данных: критерий приёмки Задачи 8. Гоняется вручную
// на выгрузке прода, в обычный прогон не входит (нет файла — тест пропускается).
//
//	SPLITTY_ROOMS=/path/rooms-export.json SPLITTY_RECON_OUT=/path/out.json \
//	  go test ./internal/service/ -run TestReconcileDebts -count=1
func TestReconcileDebts(t *testing.T) {
	src := os.Getenv("SPLITTY_ROOMS")
	if src == "" {
		t.Skip("SPLITTY_ROOMS не задан — сверка гоняется вручную по выгрузке прода")
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("не прочитал выгрузку: %v", err)
	}
	var rooms []struct {
		RoomID string   `json:"roomId"`
		Room   api.Room `json:"room"`
	}
	if err := json.Unmarshal(raw, &rooms); err != nil {
		t.Fatalf("не разобрал выгрузку: %v", err)
	}

	out := map[string][]string{}
	var failed int
	for _, r := range rooms {
		debts, err := GetRoomDebts(r.Room)
		if err != nil {
			out[r.RoomID] = []string{"ОШИБКА: " + err.Error()}
			failed++
			continue
		}
		lines := make([]string, 0, len(debts))
		for _, d := range debts {
			lines = append(lines, fmt.Sprintf("%d->%d=%d", d.Debtor.ID, d.Lender.ID, d.Sum))
		}
		sort.Strings(lines)
		out[r.RoomID] = lines
	}

	body, _ := json.MarshalIndent(out, "", " ")
	dst := os.Getenv("SPLITTY_RECON_OUT")
	if dst == "" {
		dst = "recon.json"
	}
	if err := os.WriteFile(dst, body, 0o600); err != nil {
		t.Fatalf("не записал результат: %v", err)
	}
	t.Logf("комнат %d, неисчислимых %d, записано в %s", len(rooms), failed, dst)
}
