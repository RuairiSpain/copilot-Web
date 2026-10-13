.PHONY: install install-dev scan summarise site all lint test serve
install-dev:
	pip install -r requirements-dev.txt
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
	pytest -q
	bash tests/smoke_test.sh
serve:
	python -m http.server -d site 8000
lint:
	ruff check .
