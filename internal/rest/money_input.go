package rest

import (
	"fmt"
	"net/http"

	"github.com/almaznur91/splitty/internal/api"
)

// Деньги приходят в двух полях: старом (целые единицы валюты) и минорном
// (копейки). Правило клиента простое: ЦЕЛУЮ сумму он шлёт обоими полями,
// ДРОБНУЮ — только минорным. Так сервер прежней версии откажет на дробной и
// примет целую, а не сломается на всём подряд.
//
// resolveAmount сводит эту пару к одному числу минорных единиц и по дороге
// ловит три вещи: расхождение полей, дробь при выключенном признаке и
// отсутствие обоих.

// resolveAmount возвращает сумму в минорных единицах шкалы exp.
//
// legacy — указатель, а не число с флагом: `req.Sum != 0` не отличает
// отсутствующее поле от присланного нуля, и пара {"sum":0,"sumMinor":100}
// принималась молча, хотя проекция минорного равна единице и поля расходятся.
// Присутствие поля важно именно для обнаружения конфликта.
func resolveAmount(
	field string, legacy *int, minor *int64, fractionalAllowed bool,
) (int64, *httpError) {

	if minor == nil {
		if legacy == nil {
			return 0, &httpError{http.StatusBadRequest, "validation",
				fmt.Sprintf("не указано поле %s", field)}
		}
		// Старое поле — целые единицы валюты, дробным оно быть не может.
		// Умножение проверенное: без него огромное значение сворачивалось в
		// маленькое дробное и проходило мимо лимита и мимо запрета дробей.
		v, ok := api.ToMinorChecked(*legacy)
		if !ok {
			return 0, &httpError{http.StatusBadRequest, "validation",
				fmt.Sprintf("значение поля %s вне допустимого диапазона", field)}
		}
		return v, nil
	}

	// Дробь запрещена признаком СЕРВЕРА, а не клавиатурой в приложении: запрет
	// обязан накрывать все входы разом, включая бота и разбор чека.
	if !fractionalAllowed && *minor%api.MinorFactor != 0 {
		return 0, &httpError{http.StatusBadRequest, "validation",
			"дробные суммы пока недоступны"}
	}

	// Прислали оба — они обязаны сходиться. Сходятся означает «старое поле
	// равно ПРОЕКЦИИ минорного», а не «старое × 100 == минорное»: у дробной
	// суммы второе не выполняется никогда.
	if legacy != nil && *legacy != api.FromMinor(*minor) {
		return 0, &httpError{http.StatusBadRequest, "validation",
			fmt.Sprintf("поля %s и %sMinor не сходятся", field, field)}
	}
	return *minor, nil
}

// minAmountMinor — наименьшая допустимая сумма: половина единицы валюты.
// Меньше — и старое поле, которое читают прежние сборки, оказывается нулём,
// то есть расход без суммы.
func minAmountMinor(fractionalAllowed bool) int64 {
	if fractionalAllowed {
		return api.MinorFactor / 2
	}
	// Без копеек наименьшая сумма — единица валюты: доли всё равно кратны ей.
	return api.MinorFactor
}

// maxAmountMinor — прежний потолок в единицах валюты, переведённый в минорные.
func maxAmountMinor() int64 {
	return int64(maxItemsTotal) * api.MinorFactor
}

// validateItemMoney проверяет деньги позиций чека.
//
// ⚠️ Позиции пока считаются ЦЕЛЫМИ единицами: перевод их арифметики в минорные
// — Задача 7. Поэтому минорное поле здесь принимается только ВМЕСТЕ со старым
// и только если они сходятся. Молча проигнорировать присланное минорное, как
// было раньше, нельзя: контракт выглядел бы рабочим, а точное значение
// терялось бы по дороге.
func validateItemMoney(req *operationRequest, _ bool) *httpError {
	factor := int64(api.MinorFactor)
	reject := func(msg string) *httpError {
		return &httpError{http.StatusBadRequest, "validation", msg}
	}

	for _, item := range req.Items {
		if item.PriceMinor != nil {
			if err := checkItemAmount("price", item.Price, *item.PriceMinor, factor, reject); err != nil {
				return err
			}
		}
		for _, sh := range item.Shares {
			if sh.AmountMinor == nil {
				continue
			}
			if sh.Amount == nil {
				return reject("доля позиции требует поля amount вместе с amountMinor")
			}
			if err := checkItemAmount("amount", *sh.Amount, *sh.AmountMinor, factor, reject); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkItemAmount сверяет пару полей у позиции чека.
//
// ⚠️ Требование СТРОГОЕ: минорное обязано быть ровно старым, умноженным на
// шкалу. Проекции тут мало — она сходится и у несовместимых единиц: при шкале 2
// пара price=101, priceMinor=10050 «сходится», потому что 100,50 округляется до
// 101, но арифметика позиций считает по 101, а в документе остаётся 10050, и
// позиции расходятся с итогом на полтинник.
//
// ⚠️ Дробное минорное отвергается НЕЗАВИСИМО от признака дробного ввода.
// Позиции чека считаются целыми единицами до Задачи 7, и принимать дробь,
// которую арифметика всё равно не умеет, нельзя даже с включённым признаком.
//
// ⚠️ Нулевое старое значение здесь ЗАКОННО и отсутствием не считается.
// У фиксированной доли ноль осмыслен («этот человек за позицию не платит») и
// разрешён контрактом (`parse_sanitize.go` отвергает только отрицательные), а
// присутствие поля доказано указателем у вызывающего. Ноль у цены позиции
// отвергает `validateItemizedRequest`, и дублировать его здесь незачем.
func checkItemAmount(field string, legacy int, minor, factor int64, reject func(string) *httpError) *httpError {
	if minor%factor != 0 {
		return reject("дробные суммы в позициях чека пока недоступны")
	}
	if int64(legacy)*factor != minor {
		return reject("поля " + field + " и " + field + "Minor позиции не сходятся")
	}
	return nil
}
