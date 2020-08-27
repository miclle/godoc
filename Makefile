dev:
	reflex -s -R 'Makefile' -R '.log$$' -R '_test.go$$' -R '.html$$' -R '.css$$' -R '.scss$$' -R '.js$$'\
		-- go run cmd/godoc/*.go -v

generate:
	cd static && go generate

# watch static assets, automatic generate
watch:
	cd static && reflex -s -R '.go$$'\
		-- go generate