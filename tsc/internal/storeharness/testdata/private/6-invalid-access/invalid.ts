class A {
    #x = 1;
    #m() { return 0; }
}
class B {
    #x = 2;
    f(a: A) { return a.#x; }
    g(a: A) { return a.#m(); }
}
function outside(a: A) {
    return a.#x;
}
const y = new A().#x;
