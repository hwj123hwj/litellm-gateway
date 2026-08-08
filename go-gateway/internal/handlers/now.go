package handlers

import "time"

// nowFunc is the clock used for archive timestamps. It's a package-level var
// so tests can stub it. In production it returns time.Now().
var nowFunc = time.Now
