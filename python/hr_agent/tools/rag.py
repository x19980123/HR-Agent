from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from hr_agent.config.settings import settings
from hr_agent.tools.chunking import chunk_text
from hr_agent.tools.embeddings import embedding_backend
from hr_agent.tools.rerank import rerank_docs

_SEED = [
    {
        "id": "algo-two-sum",
        "category": "algorithm",
        "title": "两数之和",
        "content": "两数之和：给定数组与目标值，返回两数下标。考察哈希表。",
        "tags": ["algorithm", "hash", "array"],
        "difficulty": "medium",
    },
    {
        "id": "algo-lrucache",
        "category": "algorithm",
        "title": "LRU Cache",
        "content": "设计 LRU Cache。考察哈希 + 双向链表。",
        "tags": ["algorithm", "design"],
        "difficulty": "medium",
    },
    {
        "id": "sys-url-shortener",
        "category": "system_design",
        "title": "短链接系统",
        "content": "设计短链接系统：生成、跳转、高并发、存储。",
        "tags": ["system_design", "distributed"],
        "difficulty": "hard",
    },
    {
        "id": "sys-mq",
        "category": "system_design",
        "title": "消息队列投递语义",
        "content": "消息队列如何保证至少一次/恰好一次投递？",
        "tags": ["system_design", "mq"],
        "difficulty": "medium",
    },
    {
        "id": "fund-mysql-index",
        "category": "fundamentals",
        "title": "MySQL 索引",
        "content": "MySQL 索引结构与最左前缀原则。",
        "tags": ["fundamentals", "mysql"],
        "difficulty": "easy",
    },
    {
        "id": "fund-go-GMP",
        "category": "fundamentals",
        "title": "Go GMP",
        "content": "解释 Go GMP 调度模型与常见阻塞场景。",
        "tags": ["fundamentals", "go"],
        "difficulty": "medium",
    },
]


def _normalize_item(raw: dict[str, Any]) -> dict[str, Any] | None:
    item_id = str(raw.get("id") or "").strip()
    content = str(raw.get("content") or raw.get("text") or "").strip()
    if not item_id or not content:
        return None
    tags = raw.get("tags") or []
    if isinstance(tags, str):
        try:
            tags = json.loads(tags)
        except Exception:
            tags = [t.strip() for t in tags.split(",") if t.strip()]
    if not isinstance(tags, list):
        tags = []
    return {
        "id": item_id,
        "category": str(raw.get("category") or "fundamentals"),
        "title": str(raw.get("title") or ""),
        "content": content,
        "tags": [str(t) for t in tags],
        "difficulty": str(raw.get("difficulty") or "medium"),
        "jd_id": str(raw.get("jd_id") or ""),
        "enabled": bool(raw.get("enabled", True)),
    }


class QuestionStore:
    def __init__(self) -> None:
        self._docs: dict[str, dict[str, Any]] = {d["id"]: dict(d) for d in _SEED}
        self._collection = None
        self._use_custom_embed = False
        self._init_chroma()
        if self._collection is not None and self._collection.count() == 0:
            for d in _SEED:
                self.upsert(d)

    def _init_chroma(self) -> None:
        try:
            import chromadb
        except Exception:
            self._collection = None
            return
        try:
            Path(settings.chroma_path).mkdir(parents=True, exist_ok=True)
            client = chromadb.PersistentClient(path=settings.chroma_path)
            # Always inject embeddings manually (DashScope / HTTP); avoid Chroma EF
            # constructor mismatches that can leave the collection unavailable.
            self._use_custom_embed = bool(settings.embedding.model and settings.embedding.api_key)
            self._collection = client.get_or_create_collection(name=settings.rag_collection)
        except Exception:
            self._collection = None
            self._use_custom_embed = False

    def _chunk_ids(self, bank_id: str) -> list[str]:
        if self._collection is None:
            return []
        try:
            # Chroma where filter by metadata
            res = self._collection.get(where={"bank_id": bank_id})
            return list(res.get("ids") or [])
        except Exception:
            # Fallback: known prefix scan not available; delete by convention ids
            return []

    def upsert(self, raw: dict[str, Any]) -> dict[str, Any]:
        item = _normalize_item(raw)
        if item is None:
            return {"ok": False, "error": "id and content required"}
        self._docs[item["id"]] = item
        if not item["enabled"]:
            self.delete(item["id"])
            return {"ok": True, "id": item["id"], "chunks": 0, "indexed": False}

        chunks = chunk_text(item["content"], settings.rag_chunk_size, settings.rag_chunk_overlap)
        if self._collection is None:
            return {"ok": True, "id": item["id"], "chunks": len(chunks), "indexed": False}

        # Remove previous chunks for this bank item
        old_ids = self._chunk_ids(item["id"])
        if not old_ids:
            # Convention ids in case where-query unsupported
            old_ids = [f"{item['id']}#c{i}" for i in range(0, 64)]
            try:
                existing = self._collection.get(ids=old_ids)
                old_ids = list(existing.get("ids") or [])
            except Exception:
                old_ids = []
        if old_ids:
            try:
                self._collection.delete(ids=old_ids)
            except Exception:
                pass

        ids = [f"{item['id']}#c{i}" for i in range(len(chunks))]
        metas = [
            {
                "bank_id": item["id"],
                "category": item["category"],
                "title": item["title"][:200],
                "difficulty": item["difficulty"],
                "jd_id": item.get("jd_id") or "",
                "chunk_index": i,
                "tags": ",".join(item["tags"])[:500],
            }
            for i in range(len(chunks))
        ]
        try:
            if self._use_custom_embed:
                from hr_agent.tools.embeddings import embed_texts

                vectors = embed_texts(chunks)
                if vectors is None:
                    # fall back: recreate collection without custom embed once
                    self._use_custom_embed = False
                    self._collection.add(ids=ids, documents=chunks, metadatas=metas)
                else:
                    self._collection.add(ids=ids, documents=chunks, metadatas=metas, embeddings=vectors)
            else:
                self._collection.add(ids=ids, documents=chunks, metadatas=metas)
        except Exception as e:
            return {"ok": False, "id": item["id"], "error": str(e)[:300]}
        return {"ok": True, "id": item["id"], "chunks": len(chunks), "indexed": True}

    def delete(self, bank_id: str) -> dict[str, Any]:
        bank_id = str(bank_id or "").strip()
        self._docs.pop(bank_id, None)
        if self._collection is None or not bank_id:
            return {"ok": True, "id": bank_id, "deleted": 0}
        ids = self._chunk_ids(bank_id)
        if not ids:
            guess = [f"{bank_id}#c{i}" for i in range(0, 64)]
            try:
                existing = self._collection.get(ids=guess)
                ids = list(existing.get("ids") or [])
            except Exception:
                ids = []
        if ids:
            try:
                self._collection.delete(ids=ids)
            except Exception:
                pass
        return {"ok": True, "id": bank_id, "deleted": len(ids)}

    def reindex(self, items: list[dict[str, Any]]) -> dict[str, Any]:
        # Reset collection so DashScope vectors replace any prior default-space data.
        try:
            import chromadb

            Path(settings.chroma_path).mkdir(parents=True, exist_ok=True)
            client = chromadb.PersistentClient(path=settings.chroma_path)
            try:
                client.delete_collection(settings.rag_collection)
            except Exception:
                pass
            self._collection = client.get_or_create_collection(name=settings.rag_collection)
            self._use_custom_embed = bool(settings.embedding.model and settings.embedding.api_key)
        except Exception:
            pass

        ok = 0
        fail = 0
        total_chunks = 0
        for raw in items:
            res = self.upsert(raw)
            if res.get("ok"):
                ok += 1
                total_chunks += int(res.get("chunks") or 0)
            else:
                fail += 1
        return {
            "ok": fail == 0,
            "upserted": ok,
            "failed": fail,
            "chunks": total_chunks,
            "collection": settings.rag_collection,
            "chroma": self._collection is not None,
            "custom_embedding": bool(self._use_custom_embed and settings.embedding.model),
            "embedding_provider": embedding_backend(),
            "rerank_model": settings.rerank_model if settings.rerank_enabled else "",
        }

    def retrieve(self, query: str, k: int = 4) -> list[dict]:
        retrieve_k = max(k, settings.rag_retrieve_k if settings.rerank_enabled else k)
        top_n = min(k, settings.rag_rerank_top_n) if settings.rerank_enabled else k
        candidates = self._retrieve_candidates(query, retrieve_k)
        if not candidates:
            return []
        if settings.rerank_enabled and len(candidates) > 1:
            return rerank_docs(query, candidates, top_n=top_n)
        return candidates[:top_n]

    def _retrieve_candidates(self, query: str, k: int) -> list[dict]:
        if self._collection is not None:
            try:
                kwargs: dict[str, Any] = {"query_texts": [query], "n_results": k}
                if self._use_custom_embed:
                    from hr_agent.tools.embeddings import embed_texts

                    qv = embed_texts([query])
                    if qv is not None:
                        kwargs = {"query_embeddings": qv, "n_results": k}
                res = self._collection.query(**kwargs)
                docs = res.get("documents", [[]])[0]
                metas = res.get("metadatas", [[]])[0]
                out = []
                seen: set[str] = set()
                for i, doc in enumerate(docs):
                    meta = metas[i] if i < len(metas) else {}
                    bank_id = str(meta.get("bank_id") or "")
                    if bank_id and bank_id in seen:
                        continue
                    if bank_id:
                        seen.add(bank_id)
                    full = self._docs.get(bank_id, {})
                    text = full.get("content") or doc
                    out.append(
                        {
                            "id": bank_id,
                            "text": text,
                            "category": meta.get("category") or full.get("category") or "fundamentals",
                            "title": meta.get("title") or full.get("title") or "",
                            "difficulty": meta.get("difficulty") or full.get("difficulty") or "medium",
                        }
                    )
                if out:
                    return out[:k]
            except Exception:
                pass
        q = (query or "").lower()
        scored = []
        for d in self._docs.values():
            if not d.get("enabled", True):
                continue
            tags = d.get("tags") or []
            score = sum(1 for t in tags if str(t).lower() in q)
            blob = (d.get("content") or "") + " " + (d.get("title") or "")
            for token in q.split():
                if token and token in blob.lower():
                    score += 1
            scored.append((score, d))
        scored.sort(key=lambda x: x[0], reverse=True)
        return [
            {
                "id": d["id"],
                "text": d["content"],
                "category": d["category"],
                "title": d.get("title") or "",
                "difficulty": d.get("difficulty") or "medium",
            }
            for _, d in scored[:k]
        ]

    def stats(self) -> dict[str, Any]:
        count = 0
        if self._collection is not None:
            try:
                count = int(self._collection.count())
            except Exception:
                count = 0
        return {
            "memory_items": len(self._docs),
            "vector_chunks": count,
            "chroma": self._collection is not None,
            "custom_embedding": bool(self._use_custom_embed and settings.embedding.model),
            "embedding_provider": embedding_backend(),
            "embedding_model": settings.embedding.model or "",
            "rerank_enabled": settings.rerank_enabled,
            "rerank_model": settings.rerank_model if settings.rerank_enabled else "",
            "retrieve_k": settings.rag_retrieve_k,
            "rerank_top_n": settings.rag_rerank_top_n,
            "collection": settings.rag_collection,
        }


store = QuestionStore()
