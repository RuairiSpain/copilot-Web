import logging
import os

import uvicorn

if __name__ == "__main__":
    logging.basicConfig(level=os.getenv("LOG_LEVEL", "INFO"), format="%(message)s")
    uvicorn.run(
        "decision_router.app:create_app",
        factory=True,
        host=os.getenv("HOST", "0.0.0.0"),
        port=int(os.getenv("PORT", "5001")),
        workers=int(os.getenv("WEB_CONCURRENCY", "1")),
    )
