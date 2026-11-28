// shareUrl is the address a customer hands on when sharing a page: the page
// itself, without its query and fragment. A query can carry the link to
// someone's order (?append=…), which works like a password
// (docs/CODE_REVIEW_FIX_PLAN.md H04).
export function shareUrl(origin: string, pathname: string): string {
  return origin + pathname
}
