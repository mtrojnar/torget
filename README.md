# torget

    $ go build torget.go
    $ ./torget -h
    torget 1.0, a fast large file downloader over locally installed Tor
    Copyright © 2021 Michał Trojnara <Michal.Trojnara@stunnel.org>
    Licensed under GNU/GPL version 3

    Usage: torget [FLAGS] URL
      -circuits int
            concurrent circuits (default 20)
      -download-timeout int
            download timeout (seconds) (default 5)
      -http-timeout int
            HTTP timeout (seconds) (default 10)
    $ ./torget https://ftp.nluug.nl/os/Linux/distr/tails/tails/stable/tails-amd64-4.23/tails-amd64-4.23.img
    Output file: tails-amd64-4.23.img
    Download length: 1185939456 bytes
      0.00% done, stalled
      0.05% done, 600.00 KB/s, ETA 0:32:55
      0.11% done, 752.00 KB/s, ETA 0:26:15
      0.23% done,   1.38 MB/s, ETA 0:14:14
      0.59% done,   4.23 MB/s, ETA 0:04:38
      0.80% done,   2.50 MB/s, ETA 0:07:49
      1.05% done,   3.02 MB/s, ETA 0:06:29
      1.28% done,   2.69 MB/s, ETA 0:07:15
      1.76% done,   5.64 MB/s, ETA 0:03:26
      2.11% done,   4.22 MB/s, ETA 0:04:35
      2.42% done,   3.70 MB/s, ETA 0:05:12
      2.82% done,   4.75 MB/s, ETA 0:04:02
      3.36% done,   6.36 MB/s, ETA 0:03:00
    Get https://ftp.nluug.nl/os/Linux/distr/tails/tails/stable/tails-amd64-4.23/tails-amd64-4.23.img: context canceled
      3.93% done,   6.78 MB/s, ETA 0:02:48
      4.64% done,   8.46 MB/s, ETA 0:02:13
      5.14% done,   5.88 MB/s, ETA 0:03:11
    ...
     98.28% done,   3.29 MB/s, ETA 0:00:06
     98.52% done,   2.84 MB/s, ETA 0:00:06
     98.68% done,   1.88 MB/s, ETA 0:00:08
     98.88% done,   2.41 MB/s, ETA 0:00:05
     99.11% done,   2.73 MB/s, ETA 0:00:03
     99.28% done,   2.01 MB/s, ETA 0:00:04
     99.44% done,   1.88 MB/s, ETA 0:00:03
     99.64% done,   2.40 MB/s, ETA 0:00:01
     99.71% done, 824.14 KB/s, ETA 0:00:04
     99.76% done, 621.65 KB/s, ETA 0:00:04
     99.84% done, 906.99 KB/s, ETA 0:00:02
     99.88% done, 499.65 KB/s, ETA 0:00:02
     99.92% done, 475.74 KB/s, ETA 0:00:01
     99.95% done, 313.94 KB/s, ETA 0:00:01
     99.96% done, 175.01 KB/s, ETA 0:00:02
     99.98% done, 206.95 KB/s, ETA 0:00:01
     99.99% done, 176.03 KB/s, ETA 0:00:00
    100.00% done,  17.71 KB/s, ETA 0:00:02
