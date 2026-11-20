/** 仅当正在看旧主站，或还没选站点时，才跟着全局主站走。 */
export function shouldFollowPrimary(currentSelected: string, previousPrimaryId: string): boolean {
  return !currentSelected || Boolean(previousPrimaryId && currentSelected === previousPrimaryId);
}
