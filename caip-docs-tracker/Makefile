.PHONY: install scan summarise site all test serve
install:
	pip install -r requirements.txt
scan:
	python -m tracker.scan
summarise:
	python -m tracker.summarise
site:
	python -m tracker.build_site
all: scan summarise site
test:
	bash tests/smoke_test.sh
serve:
	python -m http.server -d site 8000
