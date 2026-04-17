| Mode | ns/op (avg) | MB/s (avg) | B/op (avg) | allocs/op (avg) |
| --- | ---: | ---: | ---: | ---: |
| default | 3858803.0 | 17722.34 | 2647102.7 | 130.00 |
| gomaxprocs=6 | 3132616.7 | 21431.14 | 2634507.7 | 130.33 |

Latency delta (gomaxprocs=6 vs default): -18.82%
Throughput delta (gomaxprocs=6 vs default): +20.93%
