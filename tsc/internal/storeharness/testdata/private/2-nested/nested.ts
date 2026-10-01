class Outer {
    #x = 1;
    #y = true;
    m() {
        class Inner {
            #x = "inner";
            ownX() { return this.#x; }
            outerY(o: Outer) { return o.#y; }
            // Inner's #x shadows Outer's: an error on o.
            outerX(o: Outer) { return o.#x; }
        }
        return new Inner().ownX() + this.#x;
    }
}
