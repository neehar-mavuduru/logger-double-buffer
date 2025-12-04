package asynclogger

import (
	"testing"
)

func BenchmarkBuffer_Write(b *testing.B) {
	buf := NewBuffer(64*1024, 0)
	testData := []byte("test log entry")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Write(testData)
	}
}

func BenchmarkBufferSet_Write(b *testing.B) {
	set := NewBufferSet(64*1024, 8, 0)
	testData := []byte("test log entry")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		set.Write(testData)
	}
}
