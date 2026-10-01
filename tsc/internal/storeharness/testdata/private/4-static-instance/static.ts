class C {
    static #s = 1;
    #i = "i";
    static #m() { return true; }
    #n() { return 1n; }
    static get #g() { return C.#s; }
    set #t(v: string) { this.#i = v; }
    static getS() { return C.#s + C.#g; }
    getI() { this.#t = "j"; return this.#i; }
    static callM() { return C.#m(); }
    callN() { return this.#n(); }
    // An instance private through the class: an error.
    static wrong() { return C.#i; }
}
