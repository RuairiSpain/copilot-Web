"""Generic dependency graph with deterministic topological ordering."""

from __future__ import annotations

import heapq
from dataclasses import dataclass, field


class CycleError(ValueError):
    """Raised when the dependency graph contains a cycle."""

    def __init__(self, nodes: list[str]):
        self.nodes = nodes
        super().__init__("dependency cycle among: " + ", ".join(nodes))


@dataclass(frozen=True, slots=True)
class Node:
    id: str
    kind: str
    stage: int
    scope: str | None = None
    existing: bool = False


@dataclass(slots=True)
class DeploymentGraph:
    """Nodes are logical resources; an edge ``a -> b`` means *a depends on b*."""

    nodes: dict[str, Node] = field(default_factory=dict)
    deps: dict[str, set[str]] = field(default_factory=dict)

    def add_node(self, node: Node) -> Node:
        if node.id in self.nodes:
            return self.nodes[node.id]
        self.nodes[node.id] = node
        self.deps[node.id] = set()
        return node

    def has(self, node_id: str) -> bool:
        return node_id in self.nodes

    def add_dependency(self, node_id: str, depends_on: str) -> None:
        if node_id not in self.nodes:
            raise KeyError(f"unknown node '{node_id}'")
        if depends_on not in self.nodes:
            raise KeyError(f"'{node_id}' depends on unknown node '{depends_on}'")
        if node_id == depends_on:
            raise CycleError([node_id])
        self.deps[node_id].add(depends_on)

    def dependencies_of(self, node_id: str) -> list[str]:
        return sorted(self.deps[node_id])

    def dependents_of(self, node_id: str) -> list[str]:
        return sorted(n for n, d in self.deps.items() if node_id in d)

    def topological_order(self) -> list[str]:
        """Dependencies first. Ties are broken by deployment stage, then id."""
        remaining = {n: set(d) for n, d in self.deps.items()}
        dependents: dict[str, list[str]] = {n: [] for n in self.nodes}
        for node, deps in remaining.items():
            for dep in deps:
                dependents[dep].append(node)

        def key(node_id: str) -> tuple[int, str]:
            return (self.nodes[node_id].stage, node_id)

        ready = [key(n) for n, d in remaining.items() if not d]
        heapq.heapify(ready)
        order: list[str] = []
        while ready:
            _, current = heapq.heappop(ready)
            order.append(current)
            for child in dependents[current]:
                remaining[child].discard(current)
                if not remaining[child]:
                    heapq.heappush(ready, key(child))
        if len(order) != len(self.nodes):
            raise CycleError(sorted(set(self.nodes) - set(order)))
        return order

    def layers(self) -> list[list[str]]:
        """Groups of nodes that can be deployed in parallel, in order."""
        depth: dict[str, int] = {}
        for node_id in self.topological_order():
            depth[node_id] = 1 + max((depth[d] for d in self.deps[node_id]), default=-1)
        grouped: dict[int, list[str]] = {}
        for node_id, level in depth.items():
            grouped.setdefault(level, []).append(node_id)
        return [sorted(grouped[level]) for level in sorted(grouped)]
