package reserver

type Diff struct {
	Direction string
	Date      string
	Time      string
	Delta     DiffData
}

type DiffData struct {
	Count int
	Prev  int
	Delta int
}

type Chan struct {
	Direction string
	Date      string
}
