package find

import (
	"math"
	"syscall"
)

func getStatTimes(st *syscall.Stat_t) (atime, mtime, ctime float64) {
	atime = float64(st.Atim.Sec) + float64(st.Atim.Nsec)/1e9
	mtime = float64(st.Mtim.Sec) + float64(st.Mtim.Nsec)/1e9
	ctime = float64(st.Ctim.Sec) + float64(st.Ctim.Nsec)/1e9
	return
}

func ageFilterEntry(st *syscall.Stat_t, now float64, age *int64, stamp string) bool {
	if age == nil {
		return true
	}

	var fileTime float64
	switch stamp {
	case "atime":
		fileTime = float64(st.Atim.Sec)
	case "ctime":
		fileTime = float64(st.Ctim.Sec)
	default:
		fileTime = float64(st.Mtim.Sec)
	}

	diff := now - fileTime
	if *age >= 0 {
		return diff >= math.Abs(float64(*age))
	}
	return diff <= math.Abs(float64(*age))
}
