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
	field string, legacy *int, minor *int64, exp int, fractionalAllowed bool,
) (int64, *httpError) {
	factor := int64(api.MinorFactor(exp))

	if minor == nil {
		if legacy == nil {
			return 0, &httpError{http.StatusBadRequest, "validation",
				fmt.Sprintf("не указано поле %s", field)}
		}
		// Старое поле — целые единицы валюты, дробным оно быть не может.
		// Умножение проверенное: без него огромное значение сворачивалось в
		// маленькое дробное и проходило мимо лимита и мимо запрета дробей.
		v, ok := api.ToMinorChecked(*legacy, exp)
		if !ok {
			return 0, &httpError{http.StatusBadRequest, "validation",
				fmt.Sprintf("значение поля %s вне допустимого диапазона", field)}
		}
		return v, nil
	}

	// Дробь запрещена признаком СЕРВЕРА, а не клавиатурой в приложении: запрет
	// обязан накрывать все входы разом, включая бота и разбор чека.
	if !fractionalAllowed && *minor%factor != 0 {
		return 0, &httpError{http.StatusBadRequest, "validation",
			"дробные суммы пока недоступны"}
	}

	// Прислали оба — они обязаны сходиться. Сходятся означает «старое поле
	// равно ПРОЕКЦИИ минорного», а не «старое × 100 == минорное»: у дробной
	// суммы второе не выполняется никогда.
	if legacy != nil && *legacy != api.FromMinor(*minor, exp) {
		return 0, &httpError{http.StatusBadRequest, "validation",
			fmt.Sprintf("поля %s и %sMinor не сходятся", field, field)}
	}
	return *minor, nil
}

// minAmountMinor — наименьшая допустимая сумма: половина единицы валюты.
// Меньше — и старое поле, которое читают прежние сборки, оказывается нулём,
// то есть расход без суммы.
func minAmountMinor(exp int) int64 {
	factor := int64(api.MinorFactor(exp))
	if factor == 1 {
		return 1
	}
	return factor / 2
}

// maxAmountMinor — прежний потолок в единицах валюты, переведённый в минорные.
func maxAmountMinor(exp int) int64 {
	return int64(maxItemsTotal) * int64(api.MinorFactor(exp))
}

// rejectFractionalItems отвергает дробные цены и доли позиций, пока признак
// дробного ввода выключен. Позиции чека сами по себе считаются целыми
// единицами до Задачи 7, но вход всё равно обязан отказывать явно: молча
// округлить чужой чек хуже, чем не принять его.
func rejectFractionalItems(req *operationRequest, exp int, fractionalAllowed bool) *httpError {
	if fractionalAllowed {
		return nil
	}
	factor := int64(api.MinorFactor(exp))
	for _, item := range req.Items {
		if item.PriceMinor != nil && *item.PriceMinor%factor != 0 {
			return &httpError{http.StatusBadRequest, "validation", "дробные суммы пока недоступны"}
		}
		for _, sh := range item.Shares {
			if sh.AmountMinor != nil && *sh.AmountMinor%factor != 0 {
				return &httpError{http.StatusBadRequest, "validation", "дробные суммы пока недоступны"}
			}
		}
	}
	return nil
}
