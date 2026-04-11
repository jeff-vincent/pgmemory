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
      }
    ]
  }
);
print("INDEX CREATED");
printjson(db.memories.getSearchIndexes());
