import assert from "node:assert/strict";

export async function runPaginationChecks({
  test,
  state,
  requests,
  definition,
  makeReview,
  makeJob,
  navigate,
  evaluate,
  wait,
  text,
  click,
  field,
  check,
  viewport,
}) {
  async function archive(action) {
    const saved = { definitions: state.definitions, reviews: state.reviews };
    state.definitions = Array.from({ length: 251 }, (_, index) => ({
      ...structuredClone(definition),
      id: index + 1,
      revision_id: 1001 + index,
      title: `Archive migration ${index + 1}`,
    }));
    state.reviews = state.definitions.map((item) => {
      const review = makeReview(item.id, item);
      if (item.id !== 1) {
        review.status = "approved";
        review.required_groups = review.required_groups.map((group) => ({
          ...group,
          decision: "approved",
          can_decide: false,
        }));
      }
      return review;
    });
    try {
      await action();
    } finally {
      Object.assign(state, saved);
    }
  }
  const more = (label) =>
    `[...document.querySelectorAll('nav[aria-label="${label} pages"] a')].some(el => el.textContent.trim() === 'Next')`;
  await test("Jobs reach old activity and keep detail links on the selected page", async () => {
    const saved = state.jobs;
    state.jobs = Array.from({ length: 251 }, (_, index) => ({
      ...makeJob(),
      job_id: `archive-job-${251 - index}`,
      title: `Archive job ${251 - index}`,
      operation: "up",
      status: "succeeded",
    }));
    try {
      await navigate("/migrations?view=jobs&db=7", "Archive job 251");
      assert.equal(await evaluate(text("Archive job 201")), false);
      await click("Next");
      await wait(text("Archive job 201"));
      await evaluate(
        `document.querySelector('a[href*="job=archive-job-201&"]').click()`,
      );
      await wait(text("Job details"));
      assert.equal(
        await evaluate("new URLSearchParams(location.search).get('offset')"),
        "50",
      );
      assert.equal(
        await evaluate("new URLSearchParams(location.search).get('job')"),
        "archive-job-201",
      );
      await navigate("/migrations?view=jobs&db=7&offset=250", "archive-job-1");
      assert.equal(await evaluate(more("Jobs")), false);
      await evaluate(
        `document.querySelector('a[href*="job=archive-job-1&"]').click()`,
      );
      await wait(text("Job details"));
      assert.equal(
        await evaluate("new URLSearchParams(location.search).get('job')"),
        "archive-job-1",
      );
    } finally {
      state.jobs = saved;
    }
  });
  await test("Reviews reach older pages and retain browser Back/Forward", () =>
    archive(async () => {
      await navigate("/migrations?view=reviews&db=7", "Review #251 ·");
      assert.equal(
        await evaluate(
          "document.querySelectorAll('.migration-review-card').length",
        ),
        50,
      );
      assert.equal(await evaluate(text("Review #201 ·")), false);
      await click("Next");
      await wait(text("Review #201 ·"));
      await evaluate("history.back()");
      await wait(text("Review #251 ·"));
      await evaluate("history.forward()");
      await wait(text("Review #201 ·"));
      await navigate("/migrations?view=reviews&db=7&offset=250", "Review #1 ·");
      assert.equal(await evaluate(more("Reviews")), false);
      await navigate(
        "/migrations?view=reviews&db=7&offset=300",
        "No reviews on this page",
      );
      await click("Previous");
      await wait(text("Review #1 ·"));
    }));
  await test("Old review deep links fetch detail and retain leader decisions", () =>
    archive(async () => {
      const start = requests.length;
      await navigate("/migrations?view=reviews&db=7&review=1", "Review #1 ·");
      assert(
        requests
          .slice(start)
          .some((request) => request.path === "/api/migration-reviews/1"),
      );
      assert.equal(
        requests
          .slice(start)
          .some((request) => request.path === "/api/migration-reviews"),
        false,
      );
      await check("I reviewed this action");
      await click("Record approval for Editors");
      await wait(text("1 / 2 group approvals"));
    }));
  await test("Definitions paginate and open later-page records", () =>
    archive(async () => {
      await navigate(
        "/migrations?view=definitions&db=7",
        "Archive migration 251",
      );
      assert.equal(
        await evaluate(
          "document.querySelectorAll('.migration-list-entry').length",
        ),
        50,
      );
      await click("Next");
      await wait(text("Archive migration 201"));
      await click("Archive migration 201");
      await wait(text("Definition #201"));
      assert.equal(
        await evaluate("new URLSearchParams(location.search).get('offset')"),
        "50",
      );
      await navigate(
        "/migrations?view=definitions&db=7&offset=250",
        "Archive migration 1",
      );
      assert.equal(await evaluate(more("Definitions")), false);
    }));
  await test("Old drafts can be edited by ID using optimistic revisions", () =>
    archive(async () => {
      await navigate(
        "/migrations?view=definitions&db=7&definition=1",
        "Definition #1",
      );
      await field("Migration title", "Edited archived draft");
      await click("Save draft");
      await wait(text("Revision 2 · Edited archived draft"));
      const patch = requests.findLast((request) => request.method === "PATCH");
      assert.equal(patch.path, "/api/migration-definitions/1");
      assert.equal(patch.body.expected_revision_id, 1001);
    }));
  await test("Old frozen SQL copies into a separate draft", () =>
    archive(async () => {
      Object.assign(state.definitions[0], {
        editable: false,
        status: "submitted",
      });
      const original = structuredClone(state.definitions[0]);
      await navigate(
        "/migrations?view=definitions&db=7&copy=1",
        "Creating a new draft from definition #1",
      );
      await field("Migration title", "Copied archived SQL");
      await click("Save draft");
      await wait(text("Definition #252"));
      assert.deepEqual(state.definitions[0], original);
    }));
  await test("Inbox finds pending reviews beyond all newer server pages", () =>
    archive(async () => {
      const start = requests.length;
      await navigate("/migrations?view=reviews&db=7&inbox=true", "Review #1 ·");
      assert(
        requests
          .slice(start)
          .some(
            (request) =>
              request.path === "/api/migration-reviews" &&
              request.query.offset === "200",
          ),
      );
      for (const review of state.reviews)
        if (review.id % 3 === 0) {
          review.status = "pending_review";
          review.required_groups = review.required_groups.map((group) => ({
            ...group,
            decision: "pending",
            can_decide: true,
          }));
        }
      await click("Refresh reviews");
      await wait(text("Review #249 ·"));
      await click("Next");
      await wait(text("Review #99 ·"));
      await wait(text("Review #1 ·"));
      assert.equal(
        await evaluate(
          "document.querySelectorAll('.migration-review-card').length",
        ),
        34,
      );
      await viewport(375, 850);
      assert.equal(
        await evaluate("document.documentElement.scrollWidth <= innerWidth+1"),
        true,
      );
      await viewport(1440, 1000);
      await check("Only reviews awaiting my decision");
      await wait(text("Review #251 ·"));
      assert.equal(
        await evaluate("new URLSearchParams(location.search).has('offset')"),
        false,
      );
    }));
  await test("Detail errors and wrong database links do not expose mutation forms", () =>
    archive(async () => {
      await navigate(
        "/migrations?view=reviews&db=7&review=99999",
        "Review does not exist",
      );
      await navigate(
        "/migrations?view=definitions&db=7&definition=99999",
        "Definition does not exist",
      );
      assert.equal(await evaluate(text("Save draft")), false);
      state.reviews[0].database_id = 8;
      state.definitions[0].database_id = 8;
      await navigate(
        "/migrations?view=reviews&db=7&review=1",
        "This review does not belong",
      );
      assert.equal(await evaluate(text("Record approval for Editors")), false);
      await navigate(
        "/migrations?view=definitions&db=7&definition=1",
        "This definition does not belong",
      );
      assert.equal(await evaluate(text("Save draft")), false);
    }));
}
