/*

Godoc extracts and generates documentation for Go programs.

It runs as a web server and presents the documentation as a
web page.

	godoc -http=:6060

Usage:

	godoc [flag]

The flags are:

	-v
		verbose mode
	-timestamps=true
		show timestamps with directory listings
	-play=false
		enable playground
	-links=true
		link identifiers to their declarations
	-notes="BUG"
		regular expression matching note markers to show
		(e.g., "BUG|TODO", ".*")
	-goroot=$GOROOT
		Go root directory
	-http=addr
		HTTP service address (e.g., '127.0.0.1:6060' or just ':6060')
	-templates=""
		directory containing alternate template files; if set,
		the directory may provide alternative template files
		for the files in $GOROOT/lib/godoc
	-url=path
		print to standard output the data that would be served by
		an HTTP request for path
	-zip=""
		zip file providing the file system to serve; disabled if empty

By default, godoc looks at the packages it finds via $GOROOT and $GOPATH (if set).
This behavior can be altered by providing an alternative $GOROOT with the -goroot
flag.

When the -index flag is set, a search index is maintained.
The index is created at startup.

The index contains both identifier and full text search information (searchable
via regular expressions). The maximum number of full text search results shown
can be set with the -maxresults flag; if set to 0, no full text results are
shown, and only an identifier index but no full text search index is created.

By default, godoc uses the system's GOOS/GOARCH. You can provide the URL parameters
"GOOS" and "GOARCH" to set the output on the web page for the target system.

The presentation mode of web pages served by godoc can be controlled with the
"m" URL parameter; it accepts a comma-separated list of flag names as value:

	all	show documentation for all declarations, not just the exported ones
	methods	show all embedded methods, not just those of unexported anonymous fields
	src	show the original source code rather than the extracted documentation
	flat	present flat (not indented) directory listings using full paths

For instance, https://golang.org/pkg/math/big/?m=all shows the documentation
for all (not just the exported) declarations of package big.

By default, godoc serves files from the file system of the underlying OS.
Instead, a .zip file may be provided via the -zip flag, which contains
the file system to serve. The file paths stored in the .zip file must use
slash ('/') as path separator; and they must be unrooted. $GOROOT (or -goroot)
must be set to the .zip file directory path containing the Go root directory.
For instance, for a .zip file created by the command:

	zip -r go.zip $HOME/go

one may run godoc as follows:

	godoc -http=:6060 -zip=go.zip -goroot=$HOME/go

Godoc documentation is converted to HTML or to text using the go/doc package;
see https://golang.org/pkg/go/doc/#ToHTML for the exact rules.
Godoc also shows example code that is runnable by the testing package;
see https://golang.org/pkg/testing/#hdr-Examples for the conventions.
See "Godoc: documenting Go code" for how to write good comments for godoc:
https://golang.org/doc/articles/godoc_documenting_go_code.html

*/
package main // import "golang.org/x/tools/cmd/godoc"
