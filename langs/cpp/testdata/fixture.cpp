#include <string>

namespace zoo {

class Animal {
public:
  std::string name;
  virtual void speak() {}
};

class Runnable {
public:
  virtual void run() {}
};

class Dog : public Animal, public Runnable {
public:
  int tricks;
  Dog() {}
  ~Dog() {}
  void bark();
};

template <typename T>
class Box : public Container<T> {
public:
  T value;
  void push(T v) {}
};

struct Point {
  int x;
  int y;
};

} // namespace zoo

enum class Color { RED, GREEN };

void zoo::Dog::bark() {}

void free_func(int a) {}
