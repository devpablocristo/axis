.PHONY: test test-companion test-nexus test-bff test-console build-bff build-console compose-services hygiene

test: hygiene test-companion test-nexus test-bff

hygiene:
	@python3 -c 'import subprocess,sys; ac="apps/"+"console"; deny=["Cl"+"erk","VITE_"+"CLERK","COMPANION_"+"CONSOLE_PORT","NEXUS_"+"CONSOLE_PORT","Companion "+"UI","Nexus "+"console","axis/"+ac,ac,"130"+"01","130"+"02","leg"+"acy"]; cmd=["rg","-n","|".join(deny),".","-g","!**/node_modules/**","-g","!Makefile"]; out=subprocess.run(cmd,text=True,capture_output=True); print(out.stdout,end=""); sys.exit(1 if out.stdout else 0)'

test-companion:
	$(MAKE) -C companion test

test-nexus:
	$(MAKE) -C nexus test

test-bff:
	cd bff && go test ./...

test-console:
	cd console && npm run typecheck && npm run build

build-bff:
	cd bff && go build ./...

build-console:
	cd console && npm run build

compose-services:
	docker compose config --services
