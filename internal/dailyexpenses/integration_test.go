package dailyexpenses

import (
	"testing"
)

// Выгрузка расходов на сторонний хост.
//
// Адрес и список людей были вшиты в значения по умолчанию: любая поднятая
// сборка — на ноутбуке разработчика, в чужом окружении — начинала отправлять
// расходы живых людей на сторонний адрес, никого не спросив.

// TestSchedulerDoesNotStartWithoutUrl — пустой адрес выключает выгрузку.
// Проверяем через факт, что планировщик возвращается сразу и не трогает
// зависимости (они nil — обращение к ним было бы паникой).
func TestSchedulerDoesNotStartWithoutUrl(t *testing.T) {
	i := &IntegrationService{config: Config{Url: "", Users: []int{1}}}
	i.StartPostScheduler()
}

func TestSchedulerDoesNotStartWithoutUsers(t *testing.T) {
	i := &IntegrationService{config: Config{Url: "http://example.invalid/x", Users: nil}}
	i.StartPostScheduler()
}
