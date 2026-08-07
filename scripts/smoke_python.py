from hr_agent.graph.pipeline import run_parse_screen_questions
from hr_agent.nodes.classify_reply import classify_reply

text = open(r"d:\HR-Agent\samples\resume_zhangsan.txt", encoding="utf-8").read()
jd = {
    "id": "jd-backend-001",
    "title": "后端工程师",
    "requirements": {"years": 3, "skills": ["Go", "Python", "MySQL"]},
    "weights": {
        "education": 15,
        "major": 10,
        "years": 20,
        "skills": 35,
        "projects": 15,
        "papers": 5,
    },
}
out = run_parse_screen_questions("app1", "", jd, text)
print("rejected", out["rejected"], "needs_human", out["needs_human"])
print("skills", out["profile"].get("skills"))
print("score", out["screen"].get("weighted_total"))
print("q", len(out["questions"]))
c = classify_reply("我接受面试\n[thread:app1]", {})
print("intent", c.intent, c.confidence)
print("OK")
