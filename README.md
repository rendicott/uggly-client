# uggly-client
Uggly is a means to generate Terminal User Interfaces in a client-server architecture. Think of it as TUI over-the-wire (TUIOW). The client requests content from the server via gRPC protobuffers and the client handles rendering of that content. The server is sending "pages" of content one screen at a time. The protocol and page definitions take inspiration from CSS/HTML in that there are constructs such as DivBoxes, TextBlobs, Links, and Forms for example. It is opinionated in that only keyboard strokes are supported for link navigation.

There is a client compiled for Windows, Linux, and Mac. Servers can be written in any language that supports gRPC and protobuf (e.g, Go, Python, Java, etc.). New clients could be written I suppose but that's a lot of work and that's why you're here instead.

You obviously have no idea what I'm talking about so maybe a gif demo will help: 

![](./img/demo.gif)


# Why?
You may ask yourself, why is this necessary. It's not. But here are a few of the reasons why it was an interesting project:

* Make simple UI's without having to keep up with modern web nonsense.
* TUI's are cool
* Make TUI's for lots of interesting content without having to rewrite the renderer every time. 
* Using the mouse sucks
* TUI's are really cool?

The idea is that it becomes relatively trivial to host server content that can be rendered as a TUI while avoiding the pitfalls of trying to implement a TUI HTML browser which as been done many times in the past and is beholden to sites that use vanilla HTML.  HTML browsers also have to use hacky techniques to support keyboard only navigation.

This project is built heavily on [tcell](https://github.com/gdamore/tcell) which is very good but doesn't provide higher level constructs. There are a lot of TUI libraries out there, why does this project not use more robust libraries? Because, those libraries are very opinionated in how various widgets and components are used. Keeping the building blocks simple gives server authors a lot of freedom in what they can do with the client. For example, why should the client dictate that table cells all have to be the same width because that's what the table renderer on the client supports? I feel that all of the content generics and quality of life libraries should be implemented on the server side and leave the client as flexible as possible. 

NOTE: [Project Gemini](https://gemini.circumlunar.space/) seems to have similar goals as this project. Initial investigations look like it's mainly focused on raw text and doesn't support div boxes, colors, etc. However, this may be wrong. Their project includes some interesting security approaches like trust-once certificates. 

# Components
* Client (you are here)
* [Protocol](https://github.com/rendicott/uggly)
* Server (can be written with just about anything but here are a few examples)
  * [uggly-server](https://github.com/rendicott/uggly-server) - is an example of how one could host "static" content by hard coding everything in to a `pages.yml` file and then the server just serves it. Kind of like Apache serves HTML files. This is a crude implementation but is a good starting point for playing with "hello world" esque content generation.
  * [ugdyn](https://github.com/rendicott/ugdyn) - is an example of dynamically generated content and is what is showing the black/green boxes page in the gif and the Harry Potter API navigator. 

## Client Features (user'ish)
* supports TLS over gRPC which means that communication to servers that create their listeners with an SSL cert are secure. This is indicated client side via the "ugtps://" syntax and a green colored address bar. All insecure connections are represented with the "ugtp://" syntax and a red colored address-bar.
* Client doesn't have the ability to do anything to your machine except manipulate the terminal's screen. This limits some features (e.g., no file access) but also means no exploits. 
* Auto resizing of content and screen size is sent to server. Whether or not server wants to do anything about it is up to the server. 
* Variable link/keystrokes based on what the server sends. Local client upper menu bar always trumps whatever the server sends.
* Forms - Client does most of the heavy lifting for forms because it has to handle passing key event polling to the form's textboxes.
* Text wrapping of textblobs in divboxes. 
* dialing new server targets based on activated links or address-bar input
* A color demo that helps understand color names and what they look like for a given terminal. Mostly useful for server authors to select styling decisions. 
* Server authors can host a "feed" which is like a server index that can be accessed via Menu shortcut. Sometimes this is helpful for users to get their bearings on available server content. Lazy server authors could use this too if they don't want to draw fancy nav menus. 
* ability to immediately connect to a server, port, page via command parameters
* Cookie support loosely based on HTTP browser cookies. For example, a sessionID cookie provided by a server with an Expiration attribute set will store to disk on close. All cookies without Expiration set are considered session cookies and are purged on close. 
* Secure cookie storage for non-session cookies on disk on client close. This is stored in an encrypted file with the encryption key either stored in OS keyring or an ENV var that the user specifies. 
* Settings editor in browser.
* Supports Page Streams, a server can send a stream of PageResponse's giving the illusion of animation or a stream of information. Unfortunately forms on streams are not stable right now. 

## Client Notes (developer'ish)
* Common logging across all sub-packages via [log15](https://github.com/inconshreveable/log15)
* Ability to send messages to the Menu's status bar (e.g., "server timeout") with auto menu-redraw on message send. 
* browser is a monostruct with the bulk of the browser's functions being methods and properties instead of global vars. This made more and more sense as time went on as there is only one possible "screen" there's really no need to get crazy with passing all vars around to every function. Just have to be careful about multiple go-routines modifying "global" vars. Any "global" var is usually a pointer.
* error handling is terrible. Since methods can be called from many different browser states, keeping a golden thread of err return is difficult. Will need to implement an err channel of some sort and have sub-contexts check it regularly. 
* all local content (e.g., menu bar and color demo) is created using same proto structs that servers would use. The only difference is that the client can control when this content is generated and how it gets prioritized. 

# TODO:
* support more of the underlying tcell screen features such as monochrome detection
* support sounds?
* error channel with debug pane
* ability to extract text - maybe this could be done via "write to file" but would have to consider potential security concerns.
* more menu options such as "home page", file download location (if that becomes a feature), "help" pages, "about" with version, etc. Will probably need to have menu drop downs to save real-estate
* add pagination for more than one page of Feed page listings
* BUGS:
  * When you cancel out of a stream it jumps back and plays one last frame
  * Hitting refresh f5 on form submit pages clears cookies for some reason
  * settings/config editor is blinky, want to look into performance enhancements

# TODONES
* ~lock forms into divs per uggly spec~
  * done
* ~Add cookie support so custom sessions can be supported.~
* ~Add TLS to the gRPC connection. Figure out how to manage certs sanely.~ 
  * Added this but need to figure out how to get better error messages from gRPC. When all the cert stars are not aligned it just times out, e.g., you get a timeout when server doesn't provide chain. 
* ~Possibly add the concept of Page streams to support animation or gaming. Should be trivial with gRPC. Would probably add a Page streamer to proto with a time frequency between pages dictated by server with a min/max specified by client. ~
  * this is implemented and wasn't really trivial but it was mostly client side complexity in handling streams vs pages. These are still a little buggy but mostly functional. Don't try to use forms on streams for example. 
