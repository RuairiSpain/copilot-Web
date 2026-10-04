"""Unit tests for the knowledge ingestion script. No network or Azure access."""

from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace
from typing import Any

import pytest

import ingest_knowledge as ik


class FakeFiles:
    def __init__(self, store: dict[str, SimpleNamespace]) -> None:
        self._store = store

    def retrieve(self, file_id: str) -> SimpleNamespace:
        return self._store[file_id]


class FakeVectorStoreFiles:
    def __init__(self, parent: FakeClient) -> None:
        self._parent = parent
        self.uploaded: list[str] = []
        self.deleted: list[str] = []

    def list(self, vector_store_id: str) -> list[SimpleNamespace]:
        return [SimpleNamespace(id=f) for f in self._parent.attached.get(vector_store_id, [])]

    def delete(self, vector_store_id: str, file_id: str) -> None:
        self.deleted.append(file_id)
        self._parent.attached[vector_store_id].remove(file_id)

    def upload_and_poll(self, vector_store_id: str, file: Any) -> None:
        name = Path(file.name).name
        self.uploaded.append(name)
        file_id = f"file-{len(self._parent.file_objects)}"
        self._parent.file_objects[file_id] = SimpleNamespace(
            id=file_id, filename=name, bytes=len(file.read())
        )
        self._parent.attached.setdefault(vector_store_id, []).append(file_id)


class FakeVectorStores:
    def __init__(self, parent: FakeClient) -> None:
        self._parent = parent
        self.files = FakeVectorStoreFiles(parent)
        self.created: list[str] = []

    def list(self) -> list[SimpleNamespace]:
        return [SimpleNamespace(id=i, name=n) for i, n in self._parent.stores.items()]

    def create(self, name: str) -> SimpleNamespace:
        self.created.append(name)
        store_id = f"vs_{len(self._parent.stores) + 1}"
        self._parent.stores[store_id] = name
        return SimpleNamespace(id=store_id, name=name)


class FakeClient:
    def __init__(self) -> None:
        self.stores: dict[str, str] = {}
        self.attached: dict[str, list[str]] = {}
        self.file_objects: dict[str, SimpleNamespace] = {}
        self.vector_stores = FakeVectorStores(self)
        self.files = FakeFiles(self.file_objects)


@pytest.fixture
def knowledge(tmp_path: Path) -> Path:
    folder = tmp_path / "knowledge"
    folder.mkdir()
    (folder / "a.pdf").write_bytes(b"%PDF-1.4 aaaa")
    (folder / "b.PDF").write_bytes(b"%PDF-1.4 bbbbbb")
    (folder / "notes.txt").write_text("ignore me")
    return folder


class TestFindPdfs:
    def test_returns_only_pdfs_sorted(self, knowledge: Path) -> None:
        assert [p.name for p in ik.find_pdfs(knowledge)] == ["a.pdf", "b.PDF"]

    def test_missing_folder_is_an_error(self, tmp_path: Path) -> None:
        with pytest.raises(ik.IngestError, match="not found"):
            ik.find_pdfs(tmp_path / "nope")

    def test_empty_folder_is_an_error(self, tmp_path: Path) -> None:
        with pytest.raises(ik.IngestError, match="No PDF"):
            ik.find_pdfs(tmp_path)

    def test_shipped_sample_pdf_exists(self) -> None:
        names = [p.name for p in ik.find_pdfs(ik.DEFAULT_FOLDER)]
        assert "field-handbook.pdf" in names


class TestVectorStore:
    def test_creates_when_missing(self) -> None:
        client = FakeClient()
        store_id = ik.get_or_create_vector_store(client, "kb")
        assert store_id == "vs_1"
        assert client.vector_stores.created == ["kb"]

    def test_reuses_by_name(self) -> None:
        client = FakeClient()
        client.stores["vs_9"] = "kb"
        assert ik.get_or_create_vector_store(client, "kb") == "vs_9"
        assert client.vector_stores.created == []


class TestIngest:
    def test_first_run_uploads_everything(self, knowledge: Path) -> None:
        client = FakeClient()
        result = ik.ingest(client, knowledge, "kb")
        assert result.uploaded == ["a.pdf", "b.PDF"]
        assert result.skipped == [] and result.replaced == []
        assert result.vector_store_id == "vs_1"

    def test_second_run_is_a_no_op(self, knowledge: Path) -> None:
        client = FakeClient()
        ik.ingest(client, knowledge, "kb")
        result = ik.ingest(client, knowledge, "kb")
        assert result.uploaded == []
        assert result.skipped == ["a.pdf", "b.PDF"]
        assert client.vector_stores.created == ["kb"]  # created once only

    def test_changed_pdf_replaces_the_old_copy(self, knowledge: Path) -> None:
        client = FakeClient()
        ik.ingest(client, knowledge, "kb")
        (knowledge / "a.pdf").write_bytes(b"%PDF-1.4 a much longer replacement")
        result = ik.ingest(client, knowledge, "kb")
        assert result.replaced == ["a.pdf"]
        assert result.uploaded == ["a.pdf"]
        assert result.skipped == ["b.PDF"]
        assert client.vector_stores.files.deleted == ["file-0"]


class TestMain:
    def test_requires_a_project_endpoint(
        self, monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
    ) -> None:
        monkeypatch.delenv("FOUNDRY_PROJECT_ENDPOINT", raising=False)
        assert ik.main([]) == 2
        assert "FOUNDRY_PROJECT_ENDPOINT" in capsys.readouterr().err

    def test_prints_only_the_vector_store_id_to_stdout(
        self,
        monkeypatch: pytest.MonkeyPatch,
        capsys: pytest.CaptureFixture[str],
        knowledge: Path,
    ) -> None:
        fake = FakeClient()
        import azure.ai.projects as projects
        import azure.identity as identity

        class FakeProject:
            def __init__(self, endpoint: str, credential: object) -> None:
                assert endpoint == "https://example/api/projects/p"

            def get_openai_client(self) -> FakeClient:
                return fake

        monkeypatch.setattr(projects, "AIProjectClient", FakeProject)
        monkeypatch.setattr(identity, "DefaultAzureCredential", lambda: object())

        code = ik.main(
            ["--project-endpoint", "https://example/api/projects/p", "--folder", str(knowledge)]
        )
        captured = capsys.readouterr()
        assert code == 0
        assert captured.out.strip() == "vs_1"
        assert "uploaded=" in captured.err

    def test_ingest_errors_exit_non_zero(
        self,
        monkeypatch: pytest.MonkeyPatch,
        capsys: pytest.CaptureFixture[str],
        tmp_path: Path,
    ) -> None:
        import azure.ai.projects as projects
        import azure.identity as identity

        class FakeProject:
            def __init__(self, endpoint: str, credential: object) -> None: ...

            def get_openai_client(self) -> FakeClient:
                return FakeClient()

        monkeypatch.setattr(projects, "AIProjectClient", FakeProject)
        monkeypatch.setattr(identity, "DefaultAzureCredential", lambda: object())
        code = ik.main(["--project-endpoint", "https://x", "--folder", str(tmp_path / "missing")])
        assert code == 1
        assert "not found" in capsys.readouterr().err
