package push

import "testing"

// Найдено на ревью: исход ставился по «не было транзиентных сбоёв», поэтому
// при полностью отбракованных токенах (SuccessCount=0) в след писалось «sent».
// След заводился ровно ради этого случая — и врал именно в нём.
func TestOutcomeFor(t *testing.T) {
	if got := outcomeFor(0); got != OutcomeRejected {
		t.Errorf("ноль принятых токенов = %q, ожидался %q", got, OutcomeRejected)
	}
	if got := outcomeFor(1); got != OutcomeSent {
		t.Errorf("один принятый токен = %q, ожидался %q", got, OutcomeSent)
	}
	if got := outcomeFor(3); got != OutcomeSent {
		t.Errorf("три принятых токена = %q, ожидался %q", got, OutcomeSent)
	}
}
