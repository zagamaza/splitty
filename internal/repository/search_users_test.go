package repository

import (
	"context"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
)

func TestSearchUsers(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	seedUsers(t, db,
		api.User{ID: 101, Username: "zagir", DisplayName: "Загир Нургалиев"},
		api.User{ID: 102, Username: "mazanur", DisplayName: "Алмаз"},
		api.User{ID: 103, Username: "guzel_elf", DisplayName: "Гузель"},
	)
	ctx := context.Background()

	// По нику — и с собачкой, и без: человек копирует @ник как есть
	for _, q := range []string{"mazanur", "@mazanur", "MAZANUR"} {
		found, err := repo.SearchUsers(ctx, q, 10)
		if err != nil {
			t.Fatalf("поиск %q: %v", q, err)
		}
		if len(found) != 1 || found[0].ID != 102 {
			t.Errorf("поиск %q вернул %+v", q, found)
		}
	}

	// По имени, без учёта регистра
	if found, _ := repo.SearchUsers(ctx, "гузель", 10); len(found) != 1 || found[0].ID != 103 {
		t.Errorf("поиск по имени вернул %+v", found)
	}

	// По номеру — точное совпадение, а не «содержит»
	found, err := repo.SearchUsers(ctx, "101", 10)
	if err != nil {
		t.Fatalf("поиск по номеру: %v", err)
	}
	if len(found) != 1 || found[0].ID != 101 {
		t.Errorf("поиск по номеру вернул %+v", found)
	}

	// Пустой запрос — последние заведённые
	if all, _ := repo.SearchUsers(ctx, "", 10); len(all) != 3 || all[0].ID != 103 {
		t.Errorf("пустой поиск вернул %+v", all)
	}
	if two, _ := repo.SearchUsers(ctx, "", 2); len(two) != 2 {
		t.Errorf("лимит не сработал: %d", len(two))
	}
}

// Имя пишет человек: «(» из него не должно становиться синтаксисом регулярки
func TestSearchUsersEscapesRegex(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	seedUsers(t, db, api.User{ID: 201, Username: "a", DisplayName: "Алмаз (второй)"})

	if found, err := repo.SearchUsers(context.Background(), "(второй)", 10); err != nil || len(found) != 1 {
		t.Errorf("поиск по скобкам: %+v, err=%v", found, err)
	}
	if all, _ := repo.SearchUsers(context.Background(), ".*", 10); len(all) != 0 {
		t.Errorf(".* сработала как шаблон: %d", len(all))
	}
}

// Спрятанная у себя туса обязана оставаться видимой админке: «у меня пропала
// группа» чаще всего означает именно архив, и как раз её надо найти
func TestAllRoomsOfUserIncludesArchived(t *testing.T) {
	db := testDB(t)
	repo := NewRoomRepository(db)
	ctx := context.Background()

	visible := seedRoom(t, db, api.User{ID: 301, DisplayName: "Загир"})
	hidden := seedRoom(t, db, api.User{ID: 301, DisplayName: "Загир"})
	seedRoom(t, db, api.User{ID: 999, DisplayName: "Чужой"})
	if err := repo.ArchiveRoom(ctx, 301, hidden); err != nil {
		t.Fatalf("архивирование: %v", err)
	}

	rooms, err := repo.AllRoomsOfUser(ctx, 301)
	if err != nil {
		t.Fatalf("тусы человека: %v", err)
	}
	if len(rooms) != 2 {
		t.Fatalf("нашлось %d тус, ожидалось 2", len(rooms))
	}

	ids := map[string]bool{}
	for _, r := range rooms {
		ids[r.ID.Hex()] = true
	}
	if !ids[visible] || !ids[hidden] {
		t.Errorf("выдача: %v", ids)
	}

	// Чужие тусы в карточку не попадают
	if rooms, _ := repo.AllRoomsOfUser(ctx, 12345); len(rooms) != 0 {
		t.Errorf("человеку без тус нашлось %d", len(rooms))
	}
}

// Пустой список — это пустой список, а не null: клиент не должен падать на
// человеке без единой тусы
func TestAllRoomsOfUserReturnsEmptySlice(t *testing.T) {
	repo := NewRoomRepository(testDB(t))
	rooms, err := repo.AllRoomsOfUser(context.Background(), 4242)
	if err != nil {
		t.Fatal(err)
	}
	if rooms == nil {
		t.Error("вернулся nil вместо пустого списка")
	}
}
