package jwt

// func TestHeader(t *testing.T) {
// 	header := &Header{"jwt", HS256}

// 	data, err := header.Encode()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	t.Log(data)
// }

// // cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		20	 	9535766	       122.8 ns/op	     128 B/op	       3 allocs/op
// // cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		8	 	5022670	       241.7 ns/op	     128 B/op	       3 allocs/op
// func BenchmarkHeader(b *testing.B) {
// 	header := &Header{"jwt", HS256}

// 	b.ReportAllocs()
// 	b.ResetTimer()

// 	for range b.N {
// 		if _, err := header.Encode(); err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }

// // cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		1175592	       908.4 ns/op	     512 B/op	       8 allocs/op
// func BenchmarkHeader_map(b *testing.B) {
// 	for range b.N {
// 		if _, err := json.Marshal(map[string]string{"typ": "jwt", "alg": "HS256"}); err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }

// // cpu: Intel(R) Core(TM) i5-8250U CPU @ 1.60GHz		8		4140441	       283.0 ns/op	      64 B/op	       2 allocs/op
// func BenchmarkHeader_struct(b *testing.B) {
// 	for range b.N {
// 		if _, err := json.Marshal(&Header{"jwt", HS256}); err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }
