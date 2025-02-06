`mseater` is a tool for finding "good" showings of movies. That generally means
"not too close the the front/back/sides of the theater".

It's pretty fragile, but I've found it helpful.

It's also slow by design. If you bombard sites with a million simultaneous
requests, they'll rate limit you. So start it, get a coffee, then come back.

# Running with Go

You might search for showings of _Sunny_ via:

```bash
go run . --title sunny --zip 48104 --date tomorrow
```

Or install and run:

```bash
go install
mseater --title sunny --zip 48104 --date tomorrow
```

# Running with Docker

```bash
docker build -t mseater .
docker run --rm mseater --title sunny --zip 48104 --date tomorrow
```

# Prebuilt binaries

_TODO. Maybe._
