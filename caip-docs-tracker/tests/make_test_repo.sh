set -e
export GIT_EDITOR=true GIT_MERGE_AUTOEDIT=no GIT_PAGER=cat PAGER=cat
TARGET=${1:-/tmp/fake-ai-docs}; rm -rf "$TARGET" && mkdir -p "$TARGET" && cd "$TARGET" && git init -q -b main && git config user.email t@t && git config user.name "Test Author"
c(){ git add -A; GIT_AUTHOR_DATE="$1" GIT_COMMITTER_DATE="$1" git commit -q -m "$2"; }
mkdir -p articles/foundry/openai articles/search/media articles/machine-learning articles/ai-services/speech agent-framework/overview
page(){ printf -- "---\ntitle: %s\ndescription: %s\nms.service: %s\nms.topic: %s\nms.date: %s\nauthor: writer1\nms.author: writer1\n---\n\n# %s\n\n%b\n" "$2" "$3" "$4" "$5" "$6" "$2" "$7" > "$1"; }
page articles/search/vector-search-overview.md "Vector search in Azure AI Search" "Overview of vector search" azure-ai-search concept-article 09/01/2026 "Vector search lets you index embeddings."
page articles/foundry/openai/models.md "Azure OpenAI models" "Model list" azure-ai-foundry concept-article 09/01/2026 "Models include gpt-4.1."
page articles/search/legacy-indexer.md "Legacy indexer" "Old indexer" azure-ai-search how-to 09/01/2026 "This indexer is deprecated."
page articles/search/semantic-ranking.md "Semantic ranking" "Semantic ranking" azure-ai-search concept-article 09/01/2026 "Semantic ranking reorders results.\n\nSee [the overview](overview.md) for details.\n\nIt uses langauge models from Microsoft."
c "2026-09-20T10:00:00Z" "Initial content"
page articles/foundry/openai/models.md "Azure OpenAI models" "Model list" azure-ai-foundry concept-article 10/01/2026 "Models include gpt-4.1.\n\n> [!IMPORTANT]\n> gpt-4o version 2024-05-13 will be retired on 15 January 2027. Customers must migrate to gpt-4.1 before this date."
c "2026-10-01T09:00:00Z" "Announce gpt-4o retirement date (#14001)"
page articles/search/agentic-retrieval.md "Agentic retrieval in Azure AI Search" "Agentic retrieval" azure-ai-search concept-article 10/02/2026 "Agentic retrieval is now generally available in all regions that support semantic ranker. Use api-version 2026-10-01."
c "2026-10-02T15:00:00Z" "Add agentic retrieval GA article (#14010)"
sed -i 's#ms.date: 09/01/2026#ms.date: 10/06/2026#' articles/search/vector-search-overview.md
c "2026-10-06T08:00:00Z" "Update vector-search-overview.md"
page agent-framework/overview/agent-framework-overview.md "Microsoft Agent Framework overview" "Overview" agent-framework overview 10/07/2026 "The Python SDK now supports MCP tools in public preview.\n\n\`\`\`python\nagent.add_tool(mcp)\n\`\`\`"
c "2026-10-07T12:00:00Z" "Learn Editor: Update agent-framework-overview.md"
printf 'binary\0' > articles/search/media/x.png
page articles/machine-learning/how-to-managed-network.md "Managed network isolation" "Configure managed VNet" azure-machine-learning how-to 10/08/2026 "Configure a private endpoint for the workspace. Outbound rules now support FQDN rules for security."
c "2026-10-08T16:00:00Z" "Update articles/machine-learning/how-to-managed-network.md"
git checkout -q -b feature
page articles/ai-services/speech/live-interpreter.md "Live Interpreter" "Live interpreter" azure-ai-speech how-to 10/09/2026 "Live Interpreter SDK samples for C++, Java and Python."
c "2026-10-09T10:00:00Z" "Add Live Interpreter SDK samples"
git checkout -q main
GIT_COMMITTER_DATE="2026-10-09T11:00:00Z" git merge -q --no-ff --no-edit feature -m "Merge pull request #14851 from MicrosoftDocs/feature"
# Rename, delete, a binary file and an untracked file type.
git mv articles/search/vector-search-overview.md articles/search/vector-search.md
git rm -q articles/search/legacy-indexer.md
printf '%%PDF-1.4\0binary' > articles/search/diagram.pdf
echo notes > articles/search/notes.txt
c "2026-10-10T09:00:00Z" "Rename vector search article and remove legacy indexer page (#15001)"
# A TOC and an include file.
printf -- "- name: Azure AI Search\n  items:\n  - name: Vector search\n    href: vector-search.md\n" > articles/search/toc.yml
mkdir -p articles/search/includes
printf "> [!NOTE]\n> The ranker runs per query.\n" > articles/search/includes/ranker-note.md
c "2026-10-11T09:00:00Z" "Update search TOC and ranker note"
# Noise: whitespace-only, link-only and typo-sized edits.
sed -i 's/^Semantic ranking reorders results\.$/Semantic  ranking reorders results. /' articles/search/semantic-ranking.md
c "2026-10-12T08:00:00Z" "Fix formatting"
sed -i 's/(overview.md)/(search-what-is-azure-search.md)/' articles/search/semantic-ranking.md
c "2026-10-12T09:00:00Z" "Fix link"
sed -i 's/langauge/language/' articles/search/semantic-ranking.md
c "2026-10-12T10:00:00Z" "Fix typo"
git log --oneline
