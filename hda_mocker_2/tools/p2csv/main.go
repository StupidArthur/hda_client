package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"

	"github.com/parquet-go/parquet-go"
)

type Row struct {
	Scenario  string  `parquet:"scenario"`
	Tag       string  `parquet:"tag"`
	TS        int64   `parquet:"timestamp"`
	Value     float64 `parquet:"value"`
	ValueKind string  `parquet:"value_kind"`
	Quality   uint32  `parquet:"quality"`
	EventID   string  `parquet:"event_id"`
}

// parquet -> csv, 供 Python mocker 标准库读取。
// float 用最短保真表示('g' -1): NaN/±Inf/-0 均可被 Python float() 无损还原。
func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: p2csv <in.parquet> <out.csv>")
		os.Exit(1)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Println("open:", err)
		os.Exit(1)
	}
	defer f.Close()
	out, err := os.Create(os.Args[2])
	if err != nil {
		fmt.Println("create:", err)
		os.Exit(1)
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	defer w.Flush()
	w.WriteString("scenario,tag,ts_us,value,value_kind,quality\n")

	r := parquet.NewGenericReader[Row](f)
	buf := make([]Row, 8192)
	total := 0
	for {
		n, err := r.Read(buf)
		for _, row := range buf[:n] {
			w.WriteString(row.Scenario + "," + row.Tag + "," + strconv.FormatInt(row.TS, 10) + "," +
				strconv.FormatFloat(row.Value, 'g', -1, 64) + "," + row.ValueKind + "," +
				strconv.FormatUint(uint64(row.Quality), 10) + "\n")
			total++
		}
		if err != nil {
			break
		}
	}
	fmt.Printf("wrote %d rows -> %s\n", total, os.Args[2])
}
