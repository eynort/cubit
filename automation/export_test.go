package automation

import "time"

// FetchDropsForTest va a por lo que suelta el bloque block (fetchDrops).
func (j *Job) FetchDropsForTest(block string) error {
	return j.fetchDrops(func(n string) bool { return n == block }, 10)
}

// SetPickWait cambia lo que espera a que le tiren un pico (devuelve cómo dejarlo).
func SetPickWait(d time.Duration) func() {
	old := pickWait
	pickWait = d
	return func() { pickWait = old }
}

// SetSay cambia cómo escribe en el chat.
func (r *Runner) SetSay(f func(string)) { r.d.Say = f }
