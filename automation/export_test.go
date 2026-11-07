package automation

import (
	"errors"
	"time"
)

// StripMineForTest hace las galerías de un mineral hasta llevar goal.
func (j *Job) StripMineForTest(key string, goal int) error {
	os, err := ParseOres(key)
	if err != nil {
		return err
	}
	if len(os) != 1 {
		return errors.New("un mineral")
	}
	have := func() int { return j.CountWhere(oreItemOf(os)) }
	return j.stripMine(os[0], oreBlockOf(os), have, goal, nil)
}

// FetchDropsForTest va a por lo que suelta el bloque block (fetchDrops).
func (j *Job) FetchDropsForTest(block string) error {
	return j.fetchDrops(func(n string) bool { return n == block }, 10)
}

// SetStoreWait cambia lo que se espera una respuesta (devuelve cómo dejarlo).
func SetStoreWait(d time.Duration) func() {
	old := storeWait
	storeWait = d
	return func() { storeWait = old }
}

// StorageStuckForTest es storageStuck (sin cofres denegados).
func (j *Job) StorageStuckForTest(why string) bool { return j.storageStuck(why, nil) }

// NoStore dice si sigue sin guardar ni recoger.
func (j *Job) NoStore() bool { return j.storingOff() }

// SetSay cambia cómo escribe en el chat.
func (r *Runner) SetSay(f func(string)) { r.d.Say = f }

// StashForTest es stash para unos minerales (restock: antes de un viaje).
func (j *Job) StashForTest(keys string, restock bool) error {
	os, err := ParseOres(keys)
	if err != nil {
		return err
	}
	need := 0
	for _, o := range os {
		need = max(need, o.Pick)
	}
	j.pickOK = func(n string) bool { return pickTierOf(n) >= need }
	return j.stash(os, need, restock)
}
