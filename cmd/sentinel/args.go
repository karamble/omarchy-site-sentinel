package main

// leading pulls a positional argument off the front of args.
//
// flag.Parse stops at the first non-flag token, so "add <url> -name x" would
// otherwise ignore every flag after the URL.
func leading(args []string) (string, []string) {
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		return args[0], args[1:]
	}
	return "", args
}
