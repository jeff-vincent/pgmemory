// create_atlas_indexes.js — Run on Atlas proper (not local) to enable hybrid search.
// Usage: mongosh "mongodb+srv://..." --file create_atlas_indexes.js

// 1. Vector index (same as local, but with filter fields for pre-filtering).
db.memories.createSearchIndex(
  "vector_index",
  "vectorSearch",
  {
    fields: [
      {
        type: "vector",
        numDimensions: 1024,
        path: "embedding",
        similarity: "cosine"
      },
      {
        type: "filter",
        path: "quality_score"
      },
      {
        type: "filter",
        path: "source"
      }
    ]
  }
);
print("vector_index created (with quality_score + source filters)");

// 2. Full-text search index for $search hybrid queries.
db.memories.createSearchIndex(
  "text_index",
  "search",
  {
    mappings: {
      dynamic: false,
      fields: {
        content: {
          type: "string",
          analyzer: "lucene.standard"
        },
        source: {
          type: "string",
          analyzer: "lucene.keyword"
        }
      }
    }
  }
);
print("text_index created (Lucene full-text on content)");

print("\nAll Atlas indexes:");
printjson(db.memories.getSearchIndexes().toArray());
