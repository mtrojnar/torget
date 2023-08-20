# torget

    $ go build torget.go
    $ ./torget -h
    torget 1.1, a fast large file downloader over locally installed Tor
    Copyright © 2021-2023 Michał Trojnara <Michal.Trojnara@stunnel.org>
    Licensed under GNU/GPL version 3

    Usage: torget [FLAGS] URL
      -circuits int
            concurrent circuits (default 20)
      -download-timeout int
            download timeout (seconds) (default 5)
      -http-timeout int
            HTTP timeout (seconds) (default 10)
      -verbose
            diagnostic details
    $ ./torget https://download.tails.net/tails/stable/tails-amd64-5.16.1/tails-amd64-5.16.1.img
    Output file: tails-amd64-5.16.1.img
    Download length: 1326448640 bytes
    Download complete
