export const COOKIE_KEY_MAP = {
  FILM_HISTORY: "filmHistory",
};

export const cookieUtil = {
  setCookie(name: string, value: any, expire = 30) {
    if (typeof document === "undefined") return;
    const d = new Date();
    d.setTime(d.getTime() + expire * 24 * 60 * 60 * 1000);
    const expires = "expires=" + d.toUTCString();
    document.cookie =
      name + "=" + encodeURIComponent(value) + "; " + expires + "; path=/";
  },

  getCookie(name: string) {
    if (typeof document === "undefined") return "";
    const cookies = document.cookie.split("; ");
    for (let i = 0; i < cookies.length; i++) {
      const parts = cookies[i].split("=");
      if (parts[0] === name) {
        return decodeURIComponent(parts.slice(1).join("="));
      }
    }
    return "";
  },

  clearCookie(name: string) {
    if (typeof document === "undefined") return;
    document.cookie =
      name + "=; expires=Thu, 01 Jan 1970 00:00:00 UTC; path=/;";
  },
};
