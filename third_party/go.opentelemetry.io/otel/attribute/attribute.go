package attribute

type KeyValue struct {
	Key   string
	Value any
}

func String(k, v string) KeyValue  { return KeyValue{Key: k, Value: v} }
func Int(k string, v int) KeyValue { return KeyValue{Key: k, Value: v} }
